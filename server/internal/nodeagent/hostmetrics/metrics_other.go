//go:build !linux

package hostmetrics

// Collect 在非 Linux 平台不可用，返回 Available=false（生产节点为 Linux）。
func Collect() (Metrics, error) {
	return Metrics{Available: false}, nil
}
