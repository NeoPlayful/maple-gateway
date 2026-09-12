package agentconn

// Authenticator 是 CM 侧的 Agent 准入接口。
// 由上层注入具体实现（凭证校验在 enrollment 包），便于单独测试接入端。
type Authenticator interface {
	// Authenticate 校验 agent.hello 载荷：
	//   - 首注册：enrollment_token 合法 → 返回新 node_id 与新凭证（NeedCredential=true）；
	//   - 重连：node_id + node_credential 合法 → 返回既有 node_id。
	// 校验失败返回 error，接入端据此拒绝握手。
	Authenticate(hello Hello) (AuthResult, error)

	// Verify 校验已有节点在 WebSocket 上行时携带的凭证（心跳/状态上报等）。
	// 空实现可恒返回 nil（如未启用凭证时）。
	Verify(nodeID, credential string) error
}

// Hello 是接入端从 agent.hello 提取的准入信息。
type Hello struct {
	NodeID          string
	EnrollmentToken string
	Credential      string
	AgentVersion    string
	Hostname        string
	OS              string
	Arch            string
}

// AuthResult 是准入结果。
type AuthResult struct {
	NodeID         string
	Credential     string // 首次注册时下发的新凭证
	NeedCredential bool   // 是否需要在 agent.ready 中回发凭证
}

// Permissive 是宽松准入实现：始终放行，用于未启用鉴权的本地开发。
type Permissive struct{}

// Authenticate 恒放行，节点 ID 沿用 hello 携带值。
func (Permissive) Authenticate(hello Hello) (AuthResult, error) {
	return AuthResult{NodeID: hello.NodeID}, nil
}

// Verify 恒放行。
func (Permissive) Verify(string, string) error { return nil }
