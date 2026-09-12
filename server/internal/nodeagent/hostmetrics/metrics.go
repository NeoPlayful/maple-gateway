// Package hostmetrics 采集节点主机的 CPU/内存/磁盘使用率。
//
// 平台相关实现分文件：Linux 读 /proc 与 statfs（生产节点）；其他平台返回不可用，
// 保证 Agent 在开发机（Windows/macOS）也能编译运行。使用率均为 0-100 的百分比。
package hostmetrics

// Metrics 是节点主机资源使用摘要。Available=false 表示平台不支持采集。
type Metrics struct {
	Available   bool    `json:"available"`
	CPUPercent  float64 `json:"cpu_percent"`  // CPU 使用率 %
	MemTotal    int64   `json:"mem_total"`    // 内存总量（字节）
	MemUsed     int64   `json:"mem_used"`     // 已用内存（字节）
	MemPercent  float64 `json:"mem_percent"`  // 内存使用率 %
	DiskTotal   int64   `json:"disk_total"`   // 根文件系统总量（字节）
	DiskUsed    int64   `json:"disk_used"`    // 已用磁盘（字节）
	DiskPercent float64 `json:"disk_percent"` // 磁盘使用率 %
}
