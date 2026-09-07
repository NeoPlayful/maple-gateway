package proxy

// WebSocket 由 net/http 标准库 + httputil.ReverseProxy 原生支持：
//
//   - 客户端发起的 `Upgrade: websocket` 请求由 ReverseProxy 透传到上游；
//   - Go 内置对 101 Switching Protocols 的隧道式双向拷贝（见 net/http/httputil
//     ReverseProxy.handleUpgradeResponse），无需业务代码特殊处理；
//   - FlushInterval = -1 保证升级响应即时写出。
//
// 若后续需要 WebSocket 专属的心跳 / 子协议改写 / 鉴权，在此文件扩展
// Rewrite 钩子，而不改变数据面主流程。
func init() {}
