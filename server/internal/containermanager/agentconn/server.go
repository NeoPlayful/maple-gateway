package agentconn

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// Callbacks 是接入端向上层投递消息的回调集合。
// 除 OnSessionEnd 外均可为空；为空表示该类型消息暂不处理。
type Callbacks struct {
	OnSessionStart func(nodeID string)              // 握手成功、会话入表
	OnSessionEnd   func(nodeID string, kicked bool) // 会话退出（kicked=被新会话顶替）
	OnHeartbeat    func(nodeID string, p agentprotocol.HeartbeatPayload)
	OnNodeInfo     func(nodeID string, p agentprotocol.NodeInfoPayload)
	OnNodeMetrics  func(nodeID string, p agentprotocol.NodeMetricsPayload)
	OnDockerEvent  func(nodeID string, p agentprotocol.DockerEventPayload)
	OnSnapshot     func(nodeID string, p agentprotocol.ContainerSnapshotPayload)
	OnTaskAck      func(nodeID string, p agentprotocol.TaskAckPayload)
	OnTaskProgress func(nodeID string, p agentprotocol.TaskProgressPayload)
	OnTaskResult   func(nodeID string, p agentprotocol.TaskResultPayload)
	OnLogsData     func(nodeID string, p agentprotocol.LogsDataPayload)
}

// Server 是 Agent WebSocket 接入端，监听独立地址接收 Agent 反连。
type Server struct {
	listen string
	auth   Authenticator
	hub    *Hub
	cb     Callbacks
	logger *zap.Logger

	httpSrv  *http.Server
	upgrader websocket.Upgrader
}

// NewServer 构造接入端。listen 为反连监听地址（如 ":9093"）。
func NewServer(listen string, auth Authenticator, hub *Hub, cb Callbacks, logger *zap.Logger) *Server {
	return &Server{
		listen: listen,
		auth:   auth,
		hub:    hub,
		cb:     cb,
		logger: logger,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			ReadBufferSize:   4096,
			WriteBufferSize:  4096,
			// 节点汇聚层可能经反代/网关，来源校验交由凭证准入承担。
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// Serve 阻塞监听直到 ctx 结束或出错。ctx 取消时优雅关闭。
func (s *Server) Serve(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", s.handleWS)
	s.httpSrv = &http.Server{
		Addr:              s.listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", s.listen)
	if err != nil {
		return err
	}
	s.logger.Info("agent websocket listening", zap.String("addr", s.listen))

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpSrv.Shutdown(shutCtx)
	}()

	if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// handleWS 完成一次 Agent 接入：握手 → 准入 → 入表 → 收发循环。
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Debug("agent upgrade failed", zap.Error(err))
		return
	}

	res, ok := s.handshake(r.Context(), conn, remoteHost(r))
	if !ok {
		_ = conn.Close()
		return
	}
	nodeID := res.NodeID

	sess := newSession(nodeID, conn, s.logger)
	if kicked := s.hub.Add(sess); kicked != nil {
		// 同节点旧会话被顶替：关旧保新，防止命令发向失效连接。
		s.logger.Info("kicking stale agent session", zap.String("node_id", nodeID))
		kicked.close(true)
	}
	if s.cb.OnSessionStart != nil {
		s.cb.OnSessionStart(nodeID)
	}

	// 下发 agent.ready（含心跳参数；首注册时附带新凭证）。
	ready := agentprotocol.ReadyPayload{
		NodeID:       nodeID,
		HeartbeatSec: defaultHeartbeatSec,
		ServerTime:   agentprotocol.Now(),
	}
	if res.NeedCredential {
		ready.NodeCredential = res.Credential
	}
	env, _ := agentprotocol.New(agentprotocol.TypeAgentReady, "", ready)
	sess.Send(env)

	go sess.writePump()
	s.readLoop(sess)

	s.hub.Remove(sess)
	sess.close(false)
	if s.cb.OnSessionEnd != nil {
		s.cb.OnSessionEnd(nodeID, sess.Kicked())
	}
	s.logger.Info("agent session closed", zap.String("node_id", nodeID), zap.Bool("kicked", sess.Kicked()))
}

// handshake 读取首帧并要求为 agent.hello；通过准入后返回准入结果。
func (s *Server) handshake(ctx context.Context, conn *websocket.Conn, remote string) (AuthResult, bool) {
	_ = conn.SetReadDeadline(time.Now().Add(handshakeWait))
	var env agentprotocol.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		s.logger.Debug("agent handshake read failed", zap.Error(err))
		return AuthResult{}, false
	}
	if env.Type != agentprotocol.TypeAgentHello {
		s.logger.Debug("agent handshake unexpected type", zap.String("type", string(env.Type)))
		return AuthResult{}, false
	}
	var p agentprotocol.HelloPayload
	if err := env.DecodePayload(&p); err != nil {
		return AuthResult{}, false
	}
	res, err := s.auth.Authenticate(ctx, Hello{
		NodeID:          p.NodeID,
		EnrollmentToken: p.EnrollmentToken,
		Credential:      p.NodeCredential,
		AgentVersion:    p.AgentVersion,
		Hostname:        p.Hostname,
		OS:              p.OS,
		Arch:            p.Arch,
		RemoteHost:      remote,
	})
	if err != nil || res.NodeID == "" {
		s.logger.Warn("agent handshake rejected", zap.String("node_id", p.NodeID), zap.Error(err))
		return AuthResult{}, false
	}
	return res, true
}

// remoteHost 取反连连接的对端主机（不含端口）。
func remoteHost(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// readLoop 持续读取上行消息并分派；读失败即退出。
func (s *Server) readLoop(sess *Session) {
	sess.conn.SetReadLimit(maxMessageSize)
	_ = sess.conn.SetReadDeadline(time.Now().Add(pongWait))
	sess.conn.SetPongHandler(func(string) error {
		return sess.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var env agentprotocol.Envelope
		if err := sess.conn.ReadJSON(&env); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				s.logger.Debug("agent read error", zap.String("node_id", sess.NodeID), zap.Error(err))
			}
			return
		}
		_ = sess.conn.SetReadDeadline(time.Now().Add(pongWait))
		s.dispatch(sess.NodeID, env)
	}
}

// dispatch 按消息类型解码载荷并投递到对应回调。
func (s *Server) dispatch(nodeID string, env agentprotocol.Envelope) {
	switch env.Type {
	case agentprotocol.TypeHeartbeat:
		var p agentprotocol.HeartbeatPayload
		if env.DecodePayload(&p) == nil && s.cb.OnHeartbeat != nil {
			s.cb.OnHeartbeat(nodeID, p)
		}
	case agentprotocol.TypeNodeInfo:
		var p agentprotocol.NodeInfoPayload
		if env.DecodePayload(&p) == nil && s.cb.OnNodeInfo != nil {
			s.cb.OnNodeInfo(nodeID, p)
		}
	case agentprotocol.TypeNodeMetrics:
		var p agentprotocol.NodeMetricsPayload
		if env.DecodePayload(&p) == nil && s.cb.OnNodeMetrics != nil {
			s.cb.OnNodeMetrics(nodeID, p)
		}
	case agentprotocol.TypeDockerEvent:
		var p agentprotocol.DockerEventPayload
		if env.DecodePayload(&p) == nil && s.cb.OnDockerEvent != nil {
			s.cb.OnDockerEvent(nodeID, p)
		}
	case agentprotocol.TypeContainerSnap:
		var p agentprotocol.ContainerSnapshotPayload
		if env.DecodePayload(&p) == nil && s.cb.OnSnapshot != nil {
			s.cb.OnSnapshot(nodeID, p)
		}
	case agentprotocol.TypeTaskAck:
		var p agentprotocol.TaskAckPayload
		if env.DecodePayload(&p) == nil && s.cb.OnTaskAck != nil {
			s.cb.OnTaskAck(nodeID, p)
		}
	case agentprotocol.TypeTaskProgress:
		var p agentprotocol.TaskProgressPayload
		if env.DecodePayload(&p) == nil && s.cb.OnTaskProgress != nil {
			s.cb.OnTaskProgress(nodeID, p)
		}
	case agentprotocol.TypeTaskResult:
		var p agentprotocol.TaskResultPayload
		if env.DecodePayload(&p) == nil && s.cb.OnTaskResult != nil {
			s.cb.OnTaskResult(nodeID, p)
		}
	case agentprotocol.TypeLogsData:
		var p agentprotocol.LogsDataPayload
		if env.DecodePayload(&p) == nil && s.cb.OnLogsData != nil {
			s.cb.OnLogsData(nodeID, p)
		}
	default:
		s.logger.Debug("agent unknown message", zap.String("node_id", nodeID), zap.String("type", string(env.Type)))
	}
}
