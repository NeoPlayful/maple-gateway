"""Maple Gateway 数据平面验证用回显测试服务器。

支持:
  GET /echo          -> 返回请求方法/路径/Header/Query JSON
  GET /health        -> 200 ok
  GET /stream        -> 分块流式输出（验证 streaming）
  GET /sse           -> SSE 推送
  GET /slow?ms=2000  -> 延迟响应（验证 upstream timeout）
  任意大 body 请求   -> 回显 body（验证 streaming request）

用法: python echo_server.py [port]  默认 9101
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _headers_json(self):
        return {k: v for k, v in self.headers.items()}

    def do_GET(self):
        path = self.path.split("?")[0]
        if path == "/health":
            self._send(200, b'{"status":"ok"}')
        elif path == "/stream":
            self._send_stream()
        elif path == "/sse":
            self._send_sse()
        elif path.startswith("/slow"):
            ms = 2000
            q = self.path.split("?", 1)
            if len(q) > 1:
                for kv in q[1].split("&"):
                    k, _, v = kv.partition("=")
                    if k == "ms":
                        ms = int(v)
            time.sleep(ms / 1000.0)
            self._send(200, json.dumps({"slow": ms}).encode())
        elif path == "/echo":
            body = json.dumps({
                "method": self.command,
                "path": self.path,
                "headers": self._headers_json(),
            }, indent=2).encode()
            self._send(200, body)
        else:
            self._send(404, b"not found")

    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0) or 0)
        data = self.rfile.read(length)
        resp = json.dumps({
            "method": "POST",
            "path": self.path,
            "body_len": len(data),
            "body": data.decode("utf-8", "replace")[:2000],
        }).encode()
        self._send(200, resp)

    def _send_stream(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Transfer-Encoding", "chunked")
        self.end_headers()
        for i in range(5):
            chunk = f"chunk-{i}\n".encode()
            self.wfile.write(f"{len(chunk):x}\r\n".encode() + chunk + b"\r\n")
            self.wfile.flush()
            time.sleep(0.2)
        self.wfile.write(b"0\r\n\r\n")
        self.wfile.flush()

    def _send_sse(self):
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()
        for i in range(5):
            self.wfile.write(f"data: msg-{i}\n\n".encode())
            self.wfile.flush()
            time.sleep(0.3)
        self.wfile.write(b"data: done\n\n")
        self.wfile.flush()

    def _send(self, code, body):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        sys.stderr.write("%s - %s\n" % (self.address_string(), fmt % args))


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 9101
    srv = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"echo server on 127.0.0.1:{port}", flush=True)
    srv.serve_forever()


if __name__ == "__main__":
    main()
