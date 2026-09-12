//go:build !linux && !windows

package hostmetrics

// Collect 在未实现采集的平台上不可用，返回 Available=false。
// Linux 见 metrics_linux.go，Windows 见 metrics_windows.go。
func Collect() (Metrics, error) {
	return Metrics{Available: false}, nil
}
