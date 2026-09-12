package wsclient

import (
	"context"
	"encoding/json"
)

// Executor 执行一条下发的任务，返回结果载荷（JSON）或错误。
// 具体动作到 Docker 的映射由上层（agent 的 executor 实现）承担。
type Executor interface {
	Execute(ctx context.Context, action string, params json.RawMessage) (json.RawMessage, error)
}

// ExecutorFunc 便于把闭包适配为 Executor。
type ExecutorFunc func(ctx context.Context, action string, params json.RawMessage) (json.RawMessage, error)

// Execute 实现 Executor。
func (f ExecutorFunc) Execute(ctx context.Context, action string, params json.RawMessage) (json.RawMessage, error) {
	return f(ctx, action, params)
}

// Info 是 Agent 上报给 CM 的节点静态/容量信息。
// 与 agentconn 侧 NodeInfoPayload 对齐由上层负责组装。
type Info struct {
	Hostname      string
	OS            string
	Arch          string
	Kernel        string
	CPUModel      string
	CPUCores      int
	MemoryTotal   int64
	DockerVersion string
	DockerAPIVer  string
	AgentVersion  string
}
