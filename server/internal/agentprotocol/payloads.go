package agentprotocol

import "encoding/json"

// 本文件定义各类消息的 payload 结构。字段以 JSON 契约形式固定，CM 与 Agent 共用。

// ---- 连接类 ----

// HelloPayload 是 Agent 握手载荷。首次注册提交 EnrollmentToken；此后为空
// （重连时用 NodeCredential 走鉴权，不再带 token）。
type HelloPayload struct {
	EnrollmentToken string       `json:"enrollment_token,omitempty"`
	NodeID          string       `json:"node_id,omitempty"`         // 已注册节点重连时携带
	NodeCredential  string       `json:"node_credential,omitempty"` // 已注册节点重连时携带
	AgentVersion    string       `json:"agent_version,omitempty"`
	Hostname        string       `json:"hostname,omitempty"`
	OS              string       `json:"os,omitempty"`
	Arch            string       `json:"arch,omitempty"`
	Capacity        NodeCapacity `json:"capacity,omitempty"`
}

// ReadyPayload 是 CM 对 Agent 握手的回应：发放节点身份与心跳参数。
type ReadyPayload struct {
	NodeID          string `json:"node_id"`
	NodeCredential  string `json:"node_credential,omitempty"` // 首次注册时下发，Agent 需落盘
	HeartbeatSec    int    `json:"heartbeat_sec"`
	ServerTime      int64  `json:"server_time"`
}

// HeartbeatPayload 是心跳载荷。
type HeartbeatPayload struct {
	NodeID      string `json:"node_id"`
	AgentUptime int64  `json:"agent_uptime_sec,omitempty"`
}

// ---- 状态类 ----

// NodeCapacity 是节点容量摘要（握手与 node.info 复用）。
type NodeCapacity struct {
	CPUs          int    `json:"cpus,omitempty"`
	MemoryBytes   int64  `json:"memory_bytes,omitempty"`
	DockerVersion string `json:"docker_version,omitempty"`
}

// NodeInfoPayload 是节点静态信息上报。
type NodeInfoPayload struct {
	NodeID        string       `json:"node_id"`
	Hostname      string       `json:"hostname,omitempty"`
	OS            string       `json:"os,omitempty"`
	Kernel        string       `json:"kernel,omitempty"`
	Arch          string       `json:"arch,omitempty"`
	CPUModel      string       `json:"cpu_model,omitempty"`
	CPUCores      int          `json:"cpu_cores,omitempty"`
	MemoryTotal   int64        `json:"memory_total,omitempty"`
	DockerVersion string       `json:"docker_version,omitempty"`
	DockerAPIVer  string       `json:"docker_api_version,omitempty"`
	AgentVersion  string       `json:"agent_version,omitempty"`
	Capacity      NodeCapacity `json:"capacity,omitempty"`
}

// HostMetrics 是主机资源使用摘要（沿用 Node Agent 现有采集口径）。
type HostMetrics struct {
	Available   bool    `json:"available"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotal    int64   `json:"mem_total"`
	MemUsed     int64   `json:"mem_used"`
	MemPercent  float64 `json:"mem_percent"`
	DiskTotal   int64   `json:"disk_total"`
	DiskUsed    int64   `json:"disk_used"`
	DiskPercent float64 `json:"disk_percent"`
	LoadAvg1    float64 `json:"load_avg_1,omitempty"`
	UptimeSec   int64   `json:"uptime_sec,omitempty"`
}

// DockerDisk 是 Docker 引擎空间占用摘要。
type DockerDisk struct {
	LayersSize int64 `json:"layers_size"`
	Images     int   `json:"images"`
	Containers int   `json:"containers"`
	Volumes    int   `json:"volumes"`
}

// NodeMetricsPayload 是节点资源指标上报。
type NodeMetricsPayload struct {
	NodeID string      `json:"node_id"`
	Host   HostMetrics `json:"host"`
	Docker DockerDisk  `json:"docker"`
}

// DockerEventPayload 是一条 Docker 事件（start/stop/die/destroy/create/restart/health_status）。
type DockerEventPayload struct {
	NodeID      string `json:"node_id"`
	Action      string `json:"action"`
	ContainerID string `json:"container_id,omitempty"`
	InstanceID  string `json:"instance_id,omitempty"`
	Image       string `json:"image,omitempty"`
	ExitCode    int    `json:"exit_code,omitempty"`
	Time        int64  `json:"time,omitempty"` // Unix 秒
}

// ManagedContainer 是 Agent 视角的受管容器视图。
type ManagedContainer struct {
	ContainerID   string            `json:"container_id"`
	InstanceID    string            `json:"instance_id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Image         string            `json:"image,omitempty"`
	State         string            `json:"state,omitempty"`
	Status        string            `json:"status,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
	HostPort      int               `json:"host_port,omitempty"`
	ExitCode      int               `json:"exit_code,omitempty"`
	OOMKilled     bool              `json:"oom_killed,omitempty"`
	ApplicationID string            `json:"application_id,omitempty"`
	VersionID     string            `json:"version_id,omitempty"`
}

// ContainerSnapshotPayload 是受管容器全量快照（周期兜底，弥补事件丢失）。
type ContainerSnapshotPayload struct {
	NodeID     string             `json:"node_id"`
	Containers []ManagedContainer `json:"containers"`
}

// ---- 任务类 ----

// TaskExecutePayload 是下发任务的载荷。
type TaskExecutePayload struct {
	TaskID string          `json:"task_id"`
	Action string          `json:"action"`            // 见 Action 白名单
	Params json.RawMessage `json:"params,omitempty"`  // 各 action 自定义参数
}

// TaskAckPayload 是 Agent 接受任务的上报。
type TaskAckPayload struct {
	TaskID string `json:"task_id"`
}

// TaskProgressPayload 是任务进度上报。
type TaskProgressPayload struct {
	TaskID  string `json:"task_id"`
	Percent int    `json:"percent,omitempty"`
	Message string `json:"message,omitempty"`
}

// TaskResultPayload 是任务结果上报。
type TaskResultPayload struct {
	TaskID  string          `json:"task_id"`
	Status  string          `json:"status"` // success / failed / timeout / cancelled
	Error   string          `json:"error,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// TaskCancelPayload 是取消任务的下发。
type TaskCancelPayload struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason,omitempty"`
}

// ---- 日志类 ----

// LogsOpenPayload 是请求打开某容器的日志流。
type LogsOpenPayload struct {
	StreamID string `json:"stream_id"` // CM 生成，用于多路复用
	Target   string `json:"target"`    // 容器 ID 或 instance_id
	Tail     int    `json:"tail,omitempty"`
	Follow   bool   `json:"follow,omitempty"`
}

// LogsDataPayload 是日志分片。
type LogsDataPayload struct {
	StreamID string `json:"stream_id"`
	Data     string `json:"data"`
	EOF      bool   `json:"eof,omitempty"`
}

// LogsClosePayload 是关闭日志流。
type LogsClosePayload struct {
	StreamID string `json:"stream_id"`
	Reason   string `json:"reason,omitempty"`
}
