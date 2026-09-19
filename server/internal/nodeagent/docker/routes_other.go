//go:build !linux

package docker

// hostRoutes 在非 Linux 平台不做路由表扫描：Docker Desktop（Windows/macOS）的容器网络
// 运行在虚拟机内，与宿主路由表基本无关，扫描宿主路由意义不大。冲突检测退化为
// Docker 网络与接口网段两类（见 NetworkCheck）。
func hostRoutes() []string { return nil }
