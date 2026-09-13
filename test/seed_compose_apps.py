"""Compose 应用测试数据：经网关管理面创建/更新并部署，供前端「Compose 应用」页查看。

用法：python test/seed_compose_apps.py
依赖运行中的 Gateway(8090) / CM(9091) / NodeAgent / Docker。
脚本幂等：按 name 存在即覆盖更新，不存在则创建；合法规格部署，坏规格仅创建（用于校验失败演示）。
"""
import json
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
EMAIL, PASSWORD = "admin@maple.com", "admin123"


def call(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    h = {}
    if data:
        h["Content-Type"] = "application/json"
    if token:
        h["Authorization"] = "Bearer " + token
    r = urllib.request.Request(BASE + path, data=data, headers=h, method=method)
    try:
        with urllib.request.urlopen(r, timeout=180) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode())
        except Exception:
            return e.code, {}


def find(tok, name):
    _, r = call("GET", "/api/admin/cm/applications", token=tok)
    for a in r.get("data") or []:
        if a["name"] == name:
            return a["id"]
    return None


APPS = {
    "demo-stack": ("多服务演示：nginx 前端 + redis 缓存", "v1", """services:
  web:
    image: nginx:alpine
    ports:
      - "18080:80"
  cache:
    image: redis:7-alpine
    command: ["redis-server", "--port", "6399"]
""", True),
    "web-single": ("单服务：nginx", "v2", """services:
  web:
    image: nginx:alpine
    ports:
      - "18081:80"
""", True),
    "bad-spec": ("故意写坏的规格（校验/部署失败用例）", "v1", """services:
  web:
    image: nginx:alpine
    ports:
      - 18082:80
   bad: [unclosed
""", False),
}


def main():
    _, r = call("POST", "/api/auth/login", {"email": EMAIL, "password": PASSWORD})
    tok = r["data"]["token"]

    for name, (desc, ver, spec, deploy) in APPS.items():
        exist = find(tok, name)
        # 携带既有 id 提交即为覆盖更新；create 响应 id 字段恒为空（已知缺陷），故统一走回查。
        st, _ = call("POST", "/api/admin/cm/applications",
                     {"id": exist or "", "name": name, "description": desc, "version": ver, "spec": spec},
                     token=tok)
        action = "更新" if exist else "创建"
        print(f"{action} {name}: http={st}")
        if deploy:
            aid = find(tok, name)
            st, res = call("POST", f"/api/admin/cm/applications/{aid}/deploy", token=tok)
            print(f"  部署 {name}: http={st} status={(res.get('data') or {}).get('status')} msg={res.get('message', '')}")
        else:
            bid = find(tok, name)
            st, res = call("POST", f"/api/admin/cm/applications/{bid}/validate", token=tok)
            print(f"  校验 {name}: valid={(res.get('data') or {}).get('valid')} out={(res.get('data') or {}).get('output')}")

    print("\n=== 测试数据 ===")
    _, r = call("GET", "/api/admin/cm/applications", token=tok)
    for a in r.get("data") or []:
        print(f"  {a['name']:12} status={a.get('status') or '(empty)':10} version={a.get('version')} id={a['id']}")


if __name__ == "__main__":
    main()
