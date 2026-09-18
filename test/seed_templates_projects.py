"""应用模板与项目测试数据：经网关管理面创建演示模板 + 项目，实例化并部署可部署项。

供前端「应用模板」「项目」页查看演示业务。与 test/seed_compose_apps.py 的保留数据互补：
本脚本面向「模板 → 项目 → 实例化 → Application」链路；后者面向直接创建 Compose 应用。

用法：python test/seed_templates_projects.py
依赖运行中的 Gateway(8090) / CM(9091) / NodeAgent / Docker。
脚本幂等：模板按 slug、项目按租户+名称查找，存在即覆盖更新，不存在则创建；不删除任何数据。
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
EMAIL, PASSWORD = "admin@maple.com", "admin123"
DEMO_TENANT_SLUG = "t1"  # 演示数据的归属租户


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


def data(r):
    return r.get("data")


# 演示模板清单：slug -> 定义。
# 1. demo-web-stack  多服务（nginx + redis），{{port}} 未声明 → 由 CM 端口池自动分配。
# 2. demo-web-single 单服务 nginx，port 声明为参数 → 用户钉死固定宿主端口。
# 3. demo-web-persist 带数据目录挂载，演示 {{data_dir}}（节点注入数据根）+ {{data_path}}。
TEMPLATES = {
    "demo-web-stack": {
        "name": "演示：Web + 缓存",
        "description": "多服务演示：nginx 前端 + redis 缓存。宿主端口由系统自动分配。",
        "spec": """services:
  web:
    image: nginx:alpine
    ports:
      - "{{port}}:80"
  cache:
    image: redis:7-alpine
    command: ["redis-server", "--port", "6399"]
""",
        "params": [],
        "project": {"name": "demo-web-stack", "description": "多服务演示项目"},
        "values": {},
        "deploy": True,
    },
    "demo-web-single": {
        "name": "演示：单服务 Nginx",
        "description": "单服务演示：nginx，宿主端口由参数钉死为固定值。",
        "spec": """services:
  web:
    image: nginx:alpine
    ports:
      - "{{port}}:80"
""",
        "params": [
            {"key": "port", "label": "宿主端口", "type": "number",
             "required": True, "default": "18071", "hint": "钉死的固定宿主端口"},
        ],
        "project": {"name": "demo-web-single", "description": "单服务演示项目"},
        "values": {"port": "18071"},
        "deploy": True,
    },
    "demo-web-persist": {
        "name": "演示：带数据目录挂载",
        "description": "演示 {{data_dir}}（节点注入数据根）与 {{data_path}}（租户/模板/项目）拼接的绑定挂载。",
        "spec": """services:
  web:
    image: nginx:alpine
    ports:
      - "{{port}}:80"
    volumes:
      - {{data_dir}}/{{data_path}}/web:/usr/share/nginx/html
""",
        "params": [],
        "project": {"name": "demo-web-persist", "description": "数据目录挂载演示项目"},
        "values": {},
        # 节点 Agent 未配置 data_root 时，部署会被护栏拒绝（规格引用了数据根）。
        # 此条即用于演示该护栏：实例化成功、部署按预期被拒，规格里 {{data_dir}} 保留字面量。
        "deploy": False,
    },
}


def find_tenant(tok, slug):
    _, r = call("GET", "/api/admin/tenants?limit=200", token=tok)
    for t in data(r) or []:
        if t["slug"] == slug:
            return t
    return None


def find_template(tok, slug):
    _, r = call("GET", "/api/admin/templates?limit=200", token=tok)
    for t in data(r) or []:
        if t["slug"] == slug:
            return t["id"]
    return None


def find_project(tok, tenant_id, name):
    _, r = call("GET", f"/api/admin/projects?tenant_id={tenant_id}&limit=200", token=tok)
    for p in data(r) or []:
        if p["name"] == name:
            return p["id"]
    return None


def upsert_template(tok, slug, t):
    """按 slug 幂等：存在则 PATCH 覆盖，不存在则 POST 创建。返回 (id, action)。"""
    tid = find_template(tok, slug)
    body = {
        "name": t["name"], "slug": slug, "description": t["description"],
        "spec": t["spec"], "params": t["params"],
    }
    if tid:
        st, _ = call("PATCH", f"/api/admin/templates/{tid}", body, token=tok)
        return tid, ("更新" if st == 200 else f"更新失败 http={st}")
    st, r = call("POST", "/api/admin/templates", body, token=tok)
    return (data(r) or {}).get("id"), ("创建" if st == 200 else f"创建失败 http={st} msg={r.get('message','')[:60]}")


def upsert_project(tok, tenant_id, template_id, prj):
    pid = find_project(tok, tenant_id, prj["name"])
    body = {
        "tenant_id": tenant_id, "template_id": template_id,
        "name": prj["name"], "description": prj.get("description", ""),
    }
    if pid:
        st, _ = call("PATCH", f"/api/admin/projects/{pid}", {"description": prj.get("description", "")}, token=tok)
        return pid, ("复用" if st == 200 else f"更新失败 http={st}")
    st, r = call("POST", "/api/admin/projects", body, token=tok)
    return (data(r) or {}).get("id"), ("创建" if st == 200 else f"创建失败 http={st} msg={r.get('message','')[:60]}")


def main():
    st, r = call("POST", "/api/auth/login", {"email": EMAIL, "password": PASSWORD})
    tok = (data(r) or {}).get("token")
    if not tok:
        print(f"登录失败 http={st}，终止。")
        sys.exit(1)

    tenant = find_tenant(tok, DEMO_TENANT_SLUG)
    if not tenant:
        print(f"未找到租户 {DEMO_TENANT_SLUG}，终止。")
        sys.exit(1)
    tenant_id = tenant["id"]
    print(f"归租户：{tenant['name']} ({DEMO_TENANT_SLUG})\n")

    results = []
    for slug, t in TEMPLATES.items():
        tid, taction = upsert_template(tok, slug, t)
        print(f"[模板] {slug}: {taction} id={str(tid)[:8]}")
        if not tid:
            results.append((slug, None, "模板未就绪"))
            continue

        pid, paction = upsert_project(tok, tenant_id, tid, t["project"])
        print(f"[项目] {t['project']['name']}: {paction} id={str(pid)[:8]}")
        if not pid:
            results.append((slug, None, "项目未就绪"))
            continue

        st, r = call("POST", f"/api/admin/projects/{pid}/instantiate",
                     {"values": t["values"]}, token=tok)
        app_id = (data(r) or {}).get("application_id")
        print(f"[实例化] {t['project']['name']}: http={st} application_id={str(app_id)[:8]}"
              + (f" msg={r.get('message','')[:60]}" if st != 200 else ""))
        if not app_id:
            results.append((slug, None, f"实例化失败 http={st}"))
            continue

        note = "已实例化（未部署）"
        if t["deploy"]:
            st, r = call("POST", f"/api/admin/cm/applications/{app_id}/deploy", token=tok)
            status = (data(r) or {}).get("status")
            if st == 200 and status in ("running", "active"):
                note = "已部署 running"
            else:
                note = f"部署未运行 http={st} status={status} msg={r.get('message','')[:60]}"
            print(f"[部署] {t['project']['name']}: {note}")
        results.append((slug, app_id, note))
        print()

    print("=== 演示数据总览 ===")
    _, r = call("GET", "/api/admin/templates?limit=200", token=tok)
    demo_tpls = [t for t in (data(r) or []) if t["slug"].startswith("demo-web-")]
    for t in demo_tpls:
        print(f"  模板 {t['slug']:18} status={t.get('status'):9} name={t.get('name')}")

    _, r = call("GET", f"/api/admin/projects?tenant_id={tenant_id}&limit=200", token=tok)
    demo_prjs = [p for p in (data(r) or []) if p["name"].startswith("demo-web-")]
    for p in demo_prjs:
        print(f"  项目 {p['name']:18} status={p.get('status'):9} app={str(p.get('application_id'))[:8]}")


if __name__ == "__main__":
    main()
