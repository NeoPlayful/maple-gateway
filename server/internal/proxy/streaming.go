package proxy

// 流式支持说明：
//
//   - SSE / Chunked / Streaming Response：FlushInterval=-1 让 ReverseProxy
//     对每个写操作即时 flush，SSE 与分块响应不会缓冲延迟。
//   - Streaming Request：ReverseProxy 默认流式读取请求体转发，支持大文件
//     上传；http.MaxBytesReader 限制由网关上层（listener）按配置施加。
//   - 客户端断开 / Context Cancel：客户端连接断开时 request context 取消，
//     ReverseProxy 会随之取消上游请求（RoundTripper 遵守 ctx）。
//
// 这里仅作为扩展点，不在数据平面主流程加入额外逻辑。
func init() {}
