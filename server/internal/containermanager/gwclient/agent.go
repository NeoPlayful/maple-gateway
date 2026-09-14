// 本文件保留 CM 侧与 Agent 视图对齐的数据结构。
//
// 历史上这里还有一个指向 Agent 的 HTTP 客户端（AgentClient）；命令通道已统一改为
// WebSocket 任务（见 tasksys / agentregistry.Commander），HTTP 入站口在 Agent 侧可
// 完全关闭，故客户端已移除。仅保留两侧共用的视图结构。
package gwclient

// Container 是 Agent 上报的受管容器视图（与 nodeagent/docker.Container 对应）。
type Container struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	State      string            `json:"state"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	InstanceID    string            `json:"instance_id"`
	HostPort      int               `json:"host_port"`
	ContainerPort int               `json:"container_port"`
	IP            string            `json:"ip"`
	// 退出信息：非 running 容器的诊断线索。
	ExitCode   int    `json:"exit_code"`
	OOMKilled  bool   `json:"oom_killed"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// ContainerStats 是 Agent 上报的单容器资源用量视图（与 nodeagent/docker.ContainerStats 对应）。
// 速率类字段（*_bps）由 Agent 两次采样差分得出。
type ContainerStats struct {
	ContainerID string `json:"container_id"`

	CPUPercent float64 `json:"cpu_percent"`

	MemUsage   int64   `json:"mem_usage"`
	MemLimit   int64   `json:"mem_limit"`
	MemPercent float64 `json:"mem_percent"`

	NetRxBytes uint64  `json:"net_rx_bytes"`
	NetTxBytes uint64  `json:"net_tx_bytes"`
	NetRxBps   float64 `json:"net_rx_bps"`
	NetTxBps   float64 `json:"net_tx_bps"`

	BlkReadBytes  uint64  `json:"blk_read_bytes"`
	BlkWriteBytes uint64  `json:"blk_write_bytes"`
	BlkReadBps    float64 `json:"blk_read_bps"`
	BlkWriteBps   float64 `json:"blk_write_bps"`

	PidsCurrent uint64 `json:"pids_current"`
}

// CreateSpec 是容器创建规格（与 nodeagent/docker.CreateSpec 对应），
// 作为 container.create 的任务参数下发。
type CreateSpec struct {
	InstanceID   string            `json:"instance_id"`
	ServiceID    string            `json:"service_id"`
	DeploymentID string            `json:"deployment_id"`
	VersionID    string            `json:"version_id"`
	Image        string            `json:"image"`
	Port         int               `json:"port"`
	HostPort     int               `json:"host_port"`
	Env          map[string]string `json:"env,omitempty"`
	Memory       string            `json:"memory,omitempty"`
	Command      []string          `json:"command,omitempty"`
	HealthPath   string            `json:"health_path,omitempty"`
}
