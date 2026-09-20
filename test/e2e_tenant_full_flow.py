"""全流程端到端验证：新建租户 -> 模板 -> 项目 -> 实例化 -> 部署 -> 容器/实例。

与既有数据隔离——本次运行使用带时间戳的 e2e-flow-* 命名，全程不删除任何资源
（按需求保留容器），跑完打印所建资源 ID 供人工查验/后续清理。

覆盖：
  1. 新建租户并回读；
  2. 新建模板（引用内置 {{port}}，验证端口池自动分配）；
  3. 在新租户下新建项目（项目自动创建其 1:1 服务）；
  4. 实例化：渲染无残留占位符、application_id 回写、service_id 归属自动落上；
  5. 部署并观测到容器 maple-<short>-web-1；
  6. 容器注册为实例且实例 service_id 指向项目服务。

用法：python test/e2e_tenant_full_flow.py
依赖运行中的 Gateway(8090) / CM(9091) / NodeAgent / Docker。
"""
import json
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
EMAIL, PASSWORD = "admin@maple.com", "admin123"

STAMP = time.strftime("%m%d%H%M%S")
TENANT_SLUG = f"e2e-flow-{STAMP}"
TENANT_NAME = f"E2E 全流程租户 {STAMP}"
TPL_SLUG = f"e2e-flow-tpl-{STAMP}"
PROJ_NAME = f"e2e-flow-proj-{STAMP}"

SPEC = """services:
  web:
    image: nginx:alpine
    ports:
      - "{{port}}:80"
"""

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


def data(r):
    return r.get("data")


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(f"  [{'PASS' if cond else 'FAIL'}] {name}" + (f"  -> {detail}" if detail else ""))


def short_id(app_id):
    return app_id.replace("-", "")[:12]


def containers(tok):
    _, r = call("GET", "/api/admin/cm/containers", token=tok)
    return data(r) or []


def container_named(tok, name):
    for c in containers(tok):
        if c.get("name") == name:
            return c
    return None


def wait_container(tok, name, timeout=120):
    deadline = time.time() + timeout
    while time.time() < deadline:
        c = container_named(tok, name)
        if c:
            return c
        time.sleep(2)
    return None


def instance_by_id(tok, iid):
    _, r = call("GET", "/api/admin/instances?limit=500", token=tok)
    for i in data(r) or []:
        if i["id"] == iid:
            return i
    return None


def main():
    print("== 0. 登录 ==")
    _, r = call("POST", "/api/auth/login", {"email": EMAIL, "password": PASSWORD})
    tok = (data(r) or {}).get("token")
    if not tok:
        print("登录失败，终止。")
        sys.exit(1)
    print("  已登录\n")

    # ---- 1. 新建租户 ----
    print("== 1. 新建租户 ==")
    st, r = call("POST", "/api/admin/tenants",
                 {"name": TENANT_NAME, "slug": TENANT_SLUG, "description": "e2e 全流程"},
                 token=tok)
    t = data(r) or {}
    tenant_id = t.get("id")
    check("新建租户成功", st == 200 and bool(tenant_id),
          f"http={st} id={tenant_id!r}")
    if not tenant_id:
        print("创建租户失败，终止。")
        sys.exit(1)
    check("租户 slug 回读一致", t.get("slug") == TENANT_SLUG, f"slug={t.get('slug')!r}")
    st, r = call("GET", f"/api/admin/tenants/{tenant_id}", token=tok)
    check("按 ID 读取新租户", st == 200 and (data(r) or {}).get("id") == tenant_id, f"http={st}")
    print(f"  租户：{TENANT_NAME} ({TENANT_SLUG}) {tenant_id}\n")

    # ---- 2. 新建模板 ----
    print("== 2. 新建模板 ==")
    st, r = call("POST", "/api/admin/templates",
                 {"name": "E2E 全流程模板", "slug": TPL_SLUG, "description": "e2e",
                  "spec": SPEC, "params": []}, token=tok)
    tid = (data(r) or {}).get("id")
    check("新建模板成功（引用内置 port）", st == 200 and bool(tid), f"http={st} id={tid!r}")
    if not tid:
        print("创建模板失败，终止。")
        sys.exit(1)
    print(f"  模板：{TPL_SLUG} {tid}\n")

    # ---- 3. 新租户下新建项目 ----
    print("== 3. 新建项目（新租户下） ==")
    st, r = call("POST", "/api/admin/projects",
                 {"tenant_id": tenant_id, "template_id": tid,
                  "name": PROJ_NAME, "description": "e2e 全流程"}, token=tok)
    p = data(r) or {}
    pid = p.get("id")
    check("新建项目成功", st == 200 and bool(pid), f"http={st} id={pid!r} msg={r.get('message','')[:60]}")
    if not pid:
        print("创建项目失败，终止。")
        sys.exit(1)
    check("项目归属新租户", p.get("tenant_id") == tenant_id, f"tenant_id={p.get('tenant_id')!r}")
    print(f"  项目：{PROJ_NAME} {pid}\n")

    # 项目自动创建其 1:1 服务（归属该租户/项目）。
    print("== 3.1 项目自动创建的服务 ==")
    _, r = call("GET", f"/api/admin/services?project_id={pid}&limit=50", token=tok)
    svcs = data(r) or []
    check("项目自动创建了服务", len(svcs) == 1, f"count={len(svcs)}")
    svc_id = svcs[0]["id"] if svcs else None
    if svc_id:
        check("服务绑定该项目", svcs[0].get("project_id") == pid, f"project_id={svcs[0].get('project_id')!r}")
        print(f"  服务：{svcs[0].get('name')} {svc_id}\n")

    # ---- 4. 实例化 ----
    print("== 4. 实例化（渲染 + 生成 Application） ==")
    st, r = call("POST", f"/api/admin/projects/{pid}/instantiate", {"values": {}}, token=tok)
    app_id = (data(r) or {}).get("application_id")
    check("实例化成功且回写 application_id", st == 200 and bool(app_id),
          f"http={st} app_id={app_id!r} msg={r.get('message','')[:80]}")
    if not app_id:
        print("实例化失败，终止。")
        sys.exit(1)
    check("application_id 等于 project_id（约定）", app_id == pid, f"app={app_id} proj={pid}")

    st, r = call("GET", f"/api/admin/cm/applications/{app_id}", token=tok)
    app = data(r) or {}
    spec = app.get("spec") or ""
    check("渲染规格无残留占位符", "{{" not in spec, f"spec={spec!r}")
    check("内置 {{port}} 已分配为具体端口", "{{port}}" not in spec and ":80" in spec, f"spec={spec!r}")
    check("应用 service_id 归属项目服务", app.get("service_id") == svc_id,
          f"got={app.get('service_id')} want={svc_id}")
    print(f"  应用 spec：{spec.strip().replace(chr(10), ' | ')}\n")

    # ---- 5. 部署 + 观测容器 ----
    print("== 5. 部署并观测容器 ==")
    st, r = call("POST", f"/api/admin/cm/applications/{app_id}/deploy", token=tok)
    check("部署应用成功", st == 200, f"http={st} msg={r.get('message','')[:80]}")

    cname = f"maple-{short_id(app_id)}-web-1"
    c = wait_container(tok, cname)
    check("观测到容器 " + cname, c is not None, f"name={cname}")
    want_iid = None
    if c:
        want_iid = c.get("instance_id") or ""
        check("容器派生 instance_id 非空", bool(want_iid), f"iid={want_iid}")

    # ---- 6. 实例注册 ----
    print("\n== 6. 容器注册为实例 ==")
    reg = None
    if want_iid:
        deadline = time.time() + 90
        while time.time() < deadline:
            reg = instance_by_id(tok, want_iid)
            if reg:
                break
            time.sleep(3)
    check("容器注册为实例", reg is not None, f"iid={(want_iid or '')[:8]} present={reg is not None}")
    if reg:
        check("实例 service_id 指向项目服务", reg.get("service_id") == svc_id,
              f"got={reg.get('service_id')} want={svc_id}")

    # ---- 收尾：保留全部资源 ----
    print("\n== 收尾（按需求保留租户/项目/应用/容器，不清理） ==")
    print(f"  租户   : {TENANT_NAME} ({TENANT_SLUG}) {tenant_id}")
    print(f"  项目   : {PROJ_NAME} {pid}")
    print(f"  服务   : {svc_id}")
    print(f"  应用   : {app_id}")
    print(f"  容器   : {cname}")

    print(f"\n===== 结果：{len(PASS)} 通过 / {len(FAIL)} 失败 =====")
    if FAIL:
        print("失败项：" + ", ".join(FAIL))
        sys.exit(1)


if __name__ == "__main__":
    main()
