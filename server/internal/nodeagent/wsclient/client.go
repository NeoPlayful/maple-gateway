package wsclient

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"go.uber.org/zap"
)

const (
	// dialTimeout 单次连接超时。
	dialTimeout = 10 * time.Second
	// writeWait 单条消息写超时。
	writeWait = 10 * time.Second
	// pongWait 读空闲上限。
	pongWait = 90 * time.Second
	// maxMessageSize 单条下行消息大小上限。
	maxMessageSize = 1 << 20
	// minBackoff / maxBackoff 重连退避区间。
	minBackoff = 1 * time.Second
	maxBackoff = 60 * time.Second
	// defaultHeartbeat 未收到 CM 指定周期时的缺省心跳间隔。
	defaultHeartbeat = 15 * time.Second
)

// agentVersion 是 Agent 上报给 CM 的版本号（由入口在编译期/启动期可覆盖）。
var agentVersion = "dev"

// SetAgentVersion 设置上报版本号，供入口调用。
func SetAgentVersion(v string) {
	if v != "" {
		agentVersion = v
	}
}

// Conn 抽象一条到 CM 的连接，便于测试替换（真实实现基于 gorilla/websocket）。
type Conn interface {
	// WriteEnvelope 发送一条消息。
	WriteEnvelope(env agentprotocol.Envelope) error
	// ReadEnvelope 读取下一条消息。
	ReadEnvelope() (agentprotocol.Envelope, error)
	// SetReadDeadline 设置读超时。
	SetReadDeadline(t time.Time) error
	// SetWriteDeadline 设置写超时。
	SetWriteDeadline(t time.Time) error
	// Close 关闭连接。
	Close() error
}

// Dialer 建立到 CM 的连接（返回链接与 CM 期望的地址）。
type Dialer interface {
	Dial(ctx context.Context, url string) (Conn, error)
}

// Client 是 Agent 主动连接 CM 的客户端：连接 → 握手 → 心跳/任务循环 → 断线重连。
type Client struct {
	enrollee Enrollee
	executor Executor
	dialer   Dialer
	logger   *zap.Logger

	// OnConnected 在握手成功后回调（可空），用于触发一次全量状态上报。
	OnConnected func()
	// OnDisconnected 在连接断开后回调（可空）。
	OnDisconnected func()

	// HeartbeatData 返回一次心跳载荷（可空，缺省使用空载荷）。
	HeartbeatData func() agentprotocol.HeartbeatPayload

	mu        sync.Mutex
	heartbeat time.Duration
	cred      *Credential
	backoff   time.Duration

	// wmu 串行化对同一条连接的写：心跳协程与各任务协程并发写会破坏 gorilla 连接。
	wmu sync.Mutex
	// inflight 记录在途任务的可取消上下文（task_id → cancel），供 task.cancel 中断执行。
	inflight map[string]context.CancelFunc
}

// New 构造客户端。
func New(enrollee Enrollee, executor Executor, dialer Dialer, logger *zap.Logger) *Client {
	return &Client{
		enrollee:  enrollee,
		executor:  executor,
		dialer:    dialer,
		logger:    logger,
		heartbeat: defaultHeartbeat,
		backoff:   minBackoff,
	}
}

// Run 持续保持到 CM 的连接，直到 ctx 结束。断线按指数退避重连。
func (c *Client) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			c.logger.Warn("agent connection ended", zap.Error(err), zap.Duration("retry_in", c.backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.currentBackoff()):
		}
		c.bumpBackoff()
	}
}

// session 建立一次完整连接并运行收发循环，返回时表示连接结束。
func (c *Client) session(ctx context.Context) error {
	url := c.enrollee.ServerURL()
	if url == "" {
		return errors.New("server url not configured")
	}
	conn, err := c.dialer.Dial(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := c.handshake(conn); err != nil {
		return err
	}
	c.resetBackoff()
	c.logger.Info("agent connected to manager", zap.String("node_id", c.nodeID()))
	if c.OnConnected != nil {
		c.OnConnected()
	}
	if c.OnDisconnected != nil {
		defer c.OnDisconnected()
	}

	// 心跳协程：周期上行 heartbeat，写失败即结束连接。
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go c.heartbeatLoop(hbCtx, conn)

	return c.readLoop(ctx, conn)
}

// handshake 发送 agent.hello 并等待 agent.ready，据 ready 更新心跳与凭证。
func (c *Client) handshake(conn Conn) error {
	cred, err := c.enrollee.Credential()
	if err != nil {
		return err
	}
	cred = c.resolveCredential(cred)

	hello := agentprotocol.HelloPayload{
		NodeID:       cred.NodeID,
		AgentVersion: agentVersion,
		Hostname:     c.enrollee.NodeName(),
	}
	if cred.NodeID != "" {
		hello.NodeCredential = cred.NodeSecret
	} else {
		hello.EnrollmentToken = c.enrollee.EnrollmentToken()
	}

	env, err := agentprotocol.New(agentprotocol.TypeAgentHello, "", hello)
	if err != nil {
		return err
	}
	_ = conn.SetReadDeadline(time.Now().Add(dialTimeout))
	if err := conn.WriteEnvelope(env); err != nil {
		return err
	}

	ready, err := conn.ReadEnvelope()
	if err != nil {
		return err
	}
	if ready.Type != agentprotocol.TypeAgentReady {
		return errors.New("unexpected handshake response: " + string(ready.Type))
	}
	var rp agentprotocol.ReadyPayload
	if err := ready.DecodePayload(&rp); err != nil {
		return err
	}
	if rp.HeartbeatSec > 0 {
		c.setHeartbeat(time.Duration(rp.HeartbeatSec) * time.Second)
	}
	// 首注册：保存 CM 下发的凭证。
	if rp.NodeCredential != "" && rp.NodeCredential != cred.NodeSecret {
		nc := &Credential{NodeID: rp.NodeID, NodeSecret: rp.NodeCredential, ServerURL: c.enrollee.ServerURL()}
		if err := c.enrollee.Save(nc); err != nil {
			c.logger.Error("save credential failed", zap.Error(err))
		}
		c.setCred(nc)
	}
	return nil
}

// readLoop 读取下行消息并分派：任务执行/取消、日志指令、ping。
func (c *Client) readLoop(ctx context.Context, conn Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		env, err := conn.ReadEnvelope()
		if err != nil {
			return err
		}
		switch env.Type {
		case agentprotocol.TypeTaskExecute:
			c.handleTask(ctx, conn, env)
		case agentprotocol.TypeTaskCancel:
			c.handleCancel(env)
		case agentprotocol.TypeLogsOpen, agentprotocol.TypeLogsClose:
			// 日志流在后续接入；当前忽略但不报错。
		default:
			c.logger.Debug("agent unknown message", zap.String("type", string(env.Type)))
		}
	}
}

// handleTask 执行下发任务并向 CM 回报 ack / result。
func (c *Client) handleTask(ctx context.Context, conn Conn, env agentprotocol.Envelope) {
	var p agentprotocol.TaskExecutePayload
	if err := env.DecodePayload(&p); err != nil {
		return
	}
	if !agentprotocol.IsAllowedAction(p.Action) {
		c.reportResult(conn, p.TaskID, "failed", "action not allowed: "+p.Action, nil)
		return
	}
	// ack: 已接收。
	if a, err := agentprotocol.New(agentprotocol.TypeTaskAck, env.RequestID, agentprotocol.TaskAckPayload{TaskID: p.TaskID}); err == nil {
		_ = c.write(conn, a)
	}

	// 为在途任务登记可取消上下文；取消消息到达时据此中断执行。
	taskCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.inflight == nil {
		c.inflight = make(map[string]context.CancelFunc)
	}
	c.inflight[p.TaskID] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.inflight, p.TaskID)
			c.mu.Unlock()
		}()
		c.reportProgress(conn, p.TaskID, 0, "started")
		res, err := c.executor.Execute(taskCtx, p.Action, p.Params)
		if err != nil {
			if taskCtx.Err() == context.Canceled {
				c.reportResult(conn, p.TaskID, "cancelled", "task cancelled", nil)
				return
			}
			c.reportResult(conn, p.TaskID, "failed", err.Error(), nil)
			return
		}
		c.reportResult(conn, p.TaskID, "success", "", res)
	}()
}

// handleCancel 处理取消指令：中断对应在途任务的执行上下文。
func (c *Client) handleCancel(env agentprotocol.Envelope) {
	var p agentprotocol.TaskCancelPayload
	if err := env.DecodePayload(&p); err != nil {
		return
	}
	c.mu.Lock()
	cancel := c.inflight[p.TaskID]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// reportProgress 上报任务进度。
func (c *Client) reportProgress(conn Conn, taskID string, percent int, msg string) {
	p := agentprotocol.TaskProgressPayload{TaskID: taskID, Percent: percent, Message: msg}
	env, err := agentprotocol.New(agentprotocol.TypeTaskProgress, "", p)
	if err != nil {
		return
	}
	c.write(conn, env)
}

// write 串行化写：设置写超时后写出一条消息。
func (c *Client) write(conn Conn, env agentprotocol.Envelope) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteEnvelope(env)
}

// reportResult 上报任务结果。
func (c *Client) reportResult(conn Conn, taskID, status, errMsg string, res json.RawMessage) {
	p := agentprotocol.TaskResultPayload{TaskID: taskID, Status: status, Error: errMsg, Result: res}
	env, err := agentprotocol.New(agentprotocol.TypeTaskResult, "", p)
	if err != nil {
		return
	}
	if werr := c.write(conn, env); werr != nil {
		c.logger.Debug("report task result failed", zap.String("task_id", taskID), zap.Error(werr))
	}
}

// heartbeatLoop 周期发送心跳。
func (c *Client) heartbeatLoop(ctx context.Context, conn Conn) {
	interval := c.getHeartbeat()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var p agentprotocol.HeartbeatPayload
			if c.HeartbeatData != nil {
				p = c.HeartbeatData()
			}
			p.NodeID = c.nodeID()
			p.AgentUptime = int64(time.Since(start).Seconds())
			env, err := agentprotocol.New(agentprotocol.TypeHeartbeat, "", p)
			if err != nil {
				continue
			}
			if err := c.write(conn, env); err != nil {
				// 写失败：结束连接以便上层重连。
				_ = conn.Close()
				return
			}
		}
	}
}

// resolveCredential 决定本次握手用凭证还是首注册令牌。
func (c *Client) resolveCredential(local *Credential) *Credential {
	c.mu.Lock()
	cached := c.cred
	c.mu.Unlock()
	if local != nil {
		return local
	}
	if cached != nil {
		return cached
	}
	return &Credential{}
}

func (c *Client) nodeID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cred != nil {
		return c.cred.NodeID
	}
	return ""
}

func (c *Client) setCred(cred *Credential) {
	c.mu.Lock()
	c.cred = cred
	c.mu.Unlock()
}

func (c *Client) setHeartbeat(d time.Duration) {
	c.mu.Lock()
	c.heartbeat = d
	c.mu.Unlock()
}

func (c *Client) getHeartbeat() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.heartbeat <= 0 {
		return defaultHeartbeat
	}
	return c.heartbeat
}

func (c *Client) currentBackoff() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	// 加入 ±20% 抖动，避免多节点同时重连形成惊群。
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(c.backoff) * jitter)
}

func (c *Client) bumpBackoff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	next := time.Duration(math.Min(float64(c.backoff*2), float64(maxBackoff)))
	if next < minBackoff {
		next = minBackoff
	}
	c.backoff = next
}

func (c *Client) resetBackoff() {
	c.mu.Lock()
	c.backoff = minBackoff
	c.mu.Unlock()
}
