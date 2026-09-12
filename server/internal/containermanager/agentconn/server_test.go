package agentconn

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoPlayful/maple-gateway/server/internal/agentprotocol"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

// connect 建立一条到接入端的测试连接并返回底层 gorilla 连接。
func connect(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	d := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
	conn, _, err := d.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func TestHandshakeAndTaskDispatch(t *testing.T) {
	hub := NewHub()
	var gotHeartbeat, gotResult bool
	srv := NewServer("", Permissive{}, hub, Callbacks{
		OnHeartbeat:  func(string, agentprotocol.HeartbeatPayload) { gotHeartbeat = true },
		OnTaskResult: func(string, agentprotocol.TaskResultPayload) { gotResult = true },
	}, zap.NewNop())

	// 用 httptest 承载 handler，避免占用固定端口。
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", srv.handleWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/agent/ws"

	conn := connect(t, url)
	defer conn.Close()

	// 发送 agent.hello。
	hello, _ := agentprotocol.New(agentprotocol.TypeAgentHello, "", agentprotocol.HelloPayload{NodeID: "node-1"})
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	// 应收到 agent.ready。
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready agentprotocol.Envelope
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatalf("read ready: %v", err)
	}
	if ready.Type != agentprotocol.TypeAgentReady {
		t.Fatalf("want agent.ready, got %s", ready.Type)
	}

	// 等待会话入表。
	deadline := time.Now().Add(2 * time.Second)
	for !hub.IsOnline("node-1") {
		if time.Now().After(deadline) {
			t.Fatal("session not registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 上行心跳。
	hb, _ := agentprotocol.New(agentprotocol.TypeHeartbeat, "", agentprotocol.HeartbeatPayload{NodeID: "node-1"})
	_ = conn.WriteJSON(hb)

	// 下行任务：hub 的会话作为发送通道。
	sender, ok := hub.SenderFor("node-1")
	if !ok {
		t.Fatal("sender not found")
	}
	exec, _ := agentprotocol.New(agentprotocol.TypeTaskExecute, "req-1", agentprotocol.TaskExecutePayload{
		TaskID: "task-1", Action: agentprotocol.ActionContainerList,
	})
	if !sender.Send(exec) {
		t.Fatal("send task failed")
	}

	// 应收到 task.execute。
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var taskEnv agentprotocol.Envelope
	if err := conn.ReadJSON(&taskEnv); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if taskEnv.Type != agentprotocol.TypeTaskExecute {
		t.Fatalf("want task.execute, got %s", taskEnv.Type)
	}

	// 回传结果。
	res, _ := agentprotocol.New(agentprotocol.TypeTaskResult, "", agentprotocol.TaskResultPayload{TaskID: "task-1", Status: "success"})
	_ = conn.WriteJSON(res)

	deadline = time.Now().Add(2 * time.Second)
	for !(gotHeartbeat && gotResult) {
		if time.Now().After(deadline) {
			t.Fatalf("callbacks not fired: heartbeat=%v result=%v", gotHeartbeat, gotResult)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSessionKickOnReconnect(t *testing.T) {
	hub := NewHub()
	srv := NewServer("", Permissive{}, hub, Callbacks{}, zap.NewNop())
	mux := http.NewServeMux()
	mux.HandleFunc("/agent/ws", srv.handleWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/agent/ws"

	// 第一条连接。
	c1 := connect(t, url)
	defer c1.Close()
	h1, _ := agentprotocol.New(agentprotocol.TypeAgentHello, "", agentprotocol.HelloPayload{NodeID: "node-x"})
	_ = c1.WriteJSON(h1)
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var r1 agentprotocol.Envelope
	_ = c1.ReadJSON(&r1)

	// 第二条连接（同节点）：应顶替第一条。
	c2 := connect(t, url)
	defer c2.Close()
	h2, _ := agentprotocol.New(agentprotocol.TypeAgentHello, "", agentprotocol.HelloPayload{NodeID: "node-x"})
	_ = c2.WriteJSON(h2)
	c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	var r2 agentprotocol.Envelope
	if err := c2.ReadJSON(&r2); err != nil {
		t.Fatalf("second connection ready: %v", err)
	}

	// 旧连接应被关闭：读会失败。
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var junk agentprotocol.Envelope
	if err := c1.ReadJSON(&junk); err == nil {
		t.Fatal("old session should be closed after kick")
	}
}
