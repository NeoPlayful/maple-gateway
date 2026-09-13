"""Compose 应用端到端验证：断言修复后的行为。

覆盖：登录 -> 网关代理(/api/admin/cm) -> CM -> Agent -> Docker。
与 test/seed_compose_apps.py 的保留数据隔离——本脚本只用 e2e-* 命名，自建自清。

运行前提：已用含修复的代码重启 CM 与 Gateway。
用法：python test/e2e_compose_apps.py
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
PASS, FAIL = [], []


def call(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {}
    if data:
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    r = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=180) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode())
        except Exception:
            return e.code, {"raw": "unparseable"}


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(f"  [{'PASS' if cond else 'FAIL'}] {name}" + (f"  -> {detail}" if detail else ""))


def listing(tok):
    _, r = call("GET", "/api/admin/cm/applications", token=tok)
    return r.get("data") or []


def find(tok, name):
    for a in listing(tok):
        if a["name"] == name:
            return a["id"]
    return None


SPEC_DEMO = """services:
  web:
    image: nginx:alpine
    ports:
      - "18090:80"
  cache:
    image: redis:7-alpine
    command: ["redis-server", "--port", "6398"]
"""
SPEC_WEB = """services:
  web:
    image: nginx:alpine
    ports:
      - "18091:80"
"""
SPEC_BAD = """services:
  web:
    image: nginx:alpine
    ports:
      - 18092:80
   bad: [unclosed
"""

print("== 1. 登录 ==")
st, r = call("POST", "/api/auth/login", {"email": "admin@maple.com", "password": "admin123"})
tok = (r.get("data") or {}).get("token")
check("登录获取 token", st == 200 and bool(tok), f"http={st}")

print("== 2. 创建（断言响应返回非空 id —— 修复项）==")
ids = {}
for key, (name, desc, ver, spec) in {
    "demo": ("e2e-demo", "多服务 e2e（web+cache）", "v1", SPEC_DEMO),
    "web": ("e2e-web", "单服务 e2e", "v1", SPEC_WEB),
    "bad": ("e2e-bad", "坏规格 e2e", "v1", SPEC_BAD),
}.items():
    st, r = call("POST", "/api/admin/cm/applications",
                 {"name": name, "description": desc, "version": ver, "spec": spec}, token=tok)
    rid = (r.get("data") or {}).get("id")
    ids[key] = rid or find(tok, name)
    check(f"创建 {name} 返回非空 id", st == 200 and bool(rid), f"http={st} id={rid!r}")

print("== 3. 列表无重复条目（修复项：幽灵条目收敛）==")
apps = listing(tok)
id_counts, name_counts = {}, {}
for a in apps:
    id_counts[a["id"]] = id_counts.get(a["id"], 0) + 1
    name_counts[a["name"]] = name_counts.get(a["name"], 0) + 1
dup_id = {k: v for k, v in id_counts.items() if v > 1}
check("无重复 id", not dup_id, f"dup={dup_id}")
check("e2e-demo 唯一", name_counts.get("e2e-demo") == 1, f"count={name_counts.get('e2e-demo')}")

print("== 4. 校验合法 / 非法规格 ==")
st, r = call("POST", f"/api/admin/cm/applications/{ids['demo']}/validate", token=tok)
check("合法规格校验通过", st == 200 and (r.get("data") or {}).get("valid") is True, f"{r.get('data')}")
st, r = call("POST", f"/api/admin/cm/applications/{ids['bad']}/validate", token=tok)
check("非法规格校验不通过", st == 200 and (r.get("data") or {}).get("valid") is False, f"out={(r.get('data') or {}).get('output')}")

print("== 5. 部署 + ps ==")
st, r = call("POST", f"/api/admin/cm/applications/{ids['demo']}/deploy", token=tok)
check("部署 e2e-demo running", st == 200 and (r.get("data") or {}).get("status") in ("running", "active"), f"http={st}")
st, r = call("GET", f"/api/admin/cm/applications/{ids['demo']}/ps", token=tok)
svcs = (r.get("data") or {}).get("services") or []
check("ps 返回 2 服务 running", st == 200 and len(svcs) == 2 and all(s.get("state") == "running" for s in svcs),
      ", ".join(f"{s.get('service')}:{s.get('state')}" for s in svcs))

print("== 6. 停止 / 重启 ==")
st, r = call("POST", f"/api/admin/cm/applications/{ids['demo']}/stop", token=tok)
check("停止后 stopped", (r.get("data") or {}).get("status") == "stopped", f"status={(r.get('data') or {}).get('status')}")
st, r = call("POST", f"/api/admin/cm/applications/{ids['demo']}/restart", token=tok)
check("重启后 running", (r.get("data") or {}).get("status") == "running", f"status={(r.get('data') or {}).get('status')}")

print("== 7. 删除坏规格应用（修复项：可删）==")
st, r = call("DELETE", f"/api/admin/cm/applications/{ids['bad']}", token=tok)
check("坏规格应用删除成功", st == 200, f"http={st} msg={r.get('message', '')}")

print("== 8. 删除 demo / web ==")
st, r = call("DELETE", f"/api/admin/cm/applications/{ids['demo']}", token=tok)
check("e2e-demo 删除成功", st == 200, f"http={st} msg={r.get('message', '')}")
st, r = call("DELETE", f"/api/admin/cm/applications/{ids['web']}", token=tok)
check("e2e-web 删除成功", st == 200, f"http={st} msg={r.get('message', '')}")

print("== 9. 收尾：e2e-* 已清，保留数据仍在 ==")
apps = listing(tok)
names = [a["name"] for a in apps]
check("无残留 e2e-* 应用", not any(n.startswith("e2e-") for n in names), f"names={names}")
check("保留数据未被触碰", all(n in names for n in ("demo-stack", "web-single", "bad-spec")), f"names={names}")

print(f"\n===== 结果：{len(PASS)} 通过 / {len(FAIL)} 失败 =====")
if FAIL:
    print("失败项：" + ", ".join(FAIL))
    sys.exit(1)
