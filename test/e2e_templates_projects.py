"""应用模板与项目的端到端验证。

覆盖：登录 -> 模板 CRUD 与参数校验 -> 项目 CRUD 与项目标识（路径段）校验
      -> 模板渲染与实例化（生成 CM Application、回写 application_id）
      -> 数据目录字段校验 -> 绑定挂载越界拒绝。

与既有数据隔离——本脚本只用 e2e-tpl-* / e2e-proj-* / e2e-mount-* 命名，自建自清。

运行前提：Gateway 已用含本功能的代码重启（管理面 8090）。
用法：python test/e2e_templates_projects.py
"""
import json
import sys
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
PASS, FAIL = [], []

# 数据目录测试用的绝对路径（本机为 Windows）。
DATA_DIR_OK = "C:/maple-data"
DATA_DIR_REL = "relative/path"
DATA_DIR_TRAV = "/data/../etc"


def call(method, path, body=None, token=None):
    data = json.dumps(body).encode() if body is not None else None
    headers = {}
    if data:
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = "Bearer " + token
    r = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=60) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        try:
            return e.code, json.loads(e.read().decode())
        except Exception:
            return e.code, {"raw": "unparseable"}


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(f"  [{'PASS' if cond else 'FAIL'}] {name}" + (f"  -> {detail}" if detail else ""))


def data(r):
    return r.get("data")


def first_tenant(tok):
    _, r = call("GET", "/api/admin/tenants?limit=1", token=tok)
    items = data(r) or []
    return items[0] if items else None


def find_template(tok, slug):
    _, r = call("GET", "/api/admin/templates?limit=200", token=tok)
    for t in data(r) or []:
        if t["slug"] == slug:
            return t["id"]
    return None


def find_project(tok, name):
    _, r = call("GET", "/api/admin/projects?limit=200", token=tok)
    for p in data(r) or []:
        if p["name"] == name:
            return p["id"]
    return None


# 引用内置 {{data_path}} 但**不声明**该参数：按 UI 承诺应可直接使用。
SPEC_TMPL = (
    "services:\n"
    "  web:\n"
    "    image: {{image}}\n"
    "    volumes:\n"
    "      - {{data_path}}/web:/data\n"
)

SLUG_A = "e2e-tpl-a"
SLUG_B = "e2e-tpl-b"
PROJ_A = "e2e-proj-a"
PROJ_B = "e2e-proj-b"

created_tpl, created_prj, created_app = [], [], []

print("== 1. 登录 ==")
st, r = call("POST", "/api/auth/login", {"email": "admin@maple.com", "password": "admin123"})
tok = (data(r) or {}).get("token")
check("登录获取 token", st == 200 and bool(tok), f"http={st}")
if not tok:
    print("登录失败，终止。")
    sys.exit(1)

tenant = first_tenant(tok)
check("存在可用租户", bool(tenant), f"tenant={tenant and tenant.get('name')}")
if not tenant:
    sys.exit(1)
TENANT_ID, TENANT_SLUG = tenant["id"], tenant["slug"]

print("== 2. 模板 CRUD 与参数校验 ==")
# 合法模板：只声明 image（必填）；规格里的 {{data_path}} 是内置参数，无需声明。
st, r = call("POST", "/api/admin/templates", {
    "name": "E2E Template", "slug": SLUG_B, "description": "e2e",
    "spec": SPEC_TMPL,
    "params": [
        {"key": "image", "label": "镜像", "type": "string", "required": True},
    ],
}, token=tok)
tid = (data(r) or {}).get("id")
check("创建模板成功（含未声明的内置 data_path）", st == 200 and bool(tid),
      f"http={st} id={tid!r} msg={r.get('message','')[:60]}")
if tid:
    created_tpl.append(tid)

# 负向：显式声明内置键应被拒绝（其取值由系统注入，自行声明会被覆盖）。
st, r = call("POST", "/api/admin/templates", {
    "name": "builtin-decl", "slug": "e2e-tpl-builtin",
    "spec": "x: {{data_path}}",
    "params": [{"key": "data_path", "label": "x", "type": "string"}],
}, token=tok)
check("显式声明内置键 data_path 被拒绝", st == 400, f"http={st} msg={r.get('message','')[:60]}")

# 负向：slug 含非法字符（路径段校验）。
st, r = call("POST", "/api/admin/templates", {
    "name": "bad", "slug": "bad/slug", "spec": "x: 1", "params": [],
}, token=tok)
check("模板 slug 含分隔符被拒绝", st == 400, f"http={st} msg={r.get('message', '')[:50]}")

# 负向：slug 幂等冲突。
st, r = call("POST", "/api/admin/templates", {
    "name": "dup", "slug": SLUG_B, "spec": "x: 1", "params": [],
}, token=tok)
check("模板 slug 重复被拒绝", st == 409, f"http={st}")

# 负向：参数键含非法字符。
st, r = call("POST", "/api/admin/templates", {
    "name": "badkey", "slug": SLUG_A, "spec": "x: {{a-b}}",
    "params": [{"key": "a-b", "label": "x", "type": "string"}],
}, token=tok)
check("参数键含横线被拒绝", st == 400, f"http={st} msg={r.get('message', '')[:50]}")

# 负向：select 类型缺选项。
st, r = call("POST", "/api/admin/templates", {
    "name": "badsel", "slug": SLUG_A, "spec": "x: {{c}}",
    "params": [{"key": "c", "label": "x", "type": "select"}],
}, token=tok)
check("select 参数缺选项被拒绝", st == 400, f"http={st}")

st, r = call("GET", f"/api/admin/templates/{tid}", token=tok)
check("模板详情读取成功", st == 200 and (data(r) or {}).get("slug") == SLUG_B, f"http={st}")

print("== 3. 项目 CRUD 与项目标识（路径段）校验 ==")
st, r = call("POST", "/api/admin/projects", {
    "tenant_id": TENANT_ID, "template_id": tid, "name": PROJ_B, "description": "e2e",
}, token=tok)
pid = (data(r) or {}).get("id")
check("创建项目成功且返回 id", st == 200 and bool(pid), f"http={st} id={pid!r}")
if pid:
    created_prj.append(pid)

for bad_name, label in [
    ("../evil", "含上跳段"),
    ("a/b", "含斜杠"),
    ("has space", "含空格"),
    ("中文项目", "含非 ASCII"),
    ("..", "为上跳段"),
]:
    st, r = call("POST", "/api/admin/projects", {
        "tenant_id": TENANT_ID, "template_id": tid, "name": bad_name,
    }, token=tok)
    check(f"项目标识{label}被拒绝", st == 400, f"http={st} name={bad_name!r}")

# 合法字符集（下划线/连字符/点）应通过。
st, r = call("POST", "/api/admin/projects", {
    "tenant_id": TENANT_ID, "template_id": tid, "name": "e2e-proj_ok-1.0",
}, token=tok)
ok_id = (data(r) or {}).get("id")
check("项目标识含 _- 与点被接受", st == 200 and bool(ok_id), f"http={st}")
if ok_id:
    created_prj.append(ok_id)

# 同租户同名冲突。
st, r = call("POST", "/api/admin/projects", {
    "tenant_id": TENANT_ID, "template_id": tid, "name": PROJ_B,
}, token=tok)
check("同租户同名项目被拒绝", st == 409, f"http={st}")

# 缺模板。
st, r = call("POST", "/api/admin/projects", {
    "tenant_id": TENANT_ID, "name": "e2e-proj-notpl",
}, token=tok)
check("项目缺模板被拒绝", st == 400, f"http={st}")

print("== 4. 模板渲染与实例化 ==")
st, r = call("POST", f"/api/admin/projects/{pid}/instantiate",
             {"values": {"image": "nginx:alpine"}}, token=tok)
app_id = (data(r) or {}).get("application_id")
check("实例化成功且回写 application_id", st == 200 and bool(app_id), f"http={st} app_id={app_id!r} msg={r.get('message','')[:60]}")
if app_id:
    created_app.append(app_id)

# 断言渲染结果：占位符全部替换（无残留 {{）。
if app_id:
    st, r = call("GET", f"/api/admin/cm/applications/{app_id}", token=tok)
    spec = (data(r) or {}).get("spec") or ""
    expected_path = f"{TENANT_SLUG}/{SLUG_B}/{PROJ_B}/web"
    check("渲染规格无残留占位符", "{{" not in spec, f"spec={spec!r}")
    check("data_path 注入为 租户/模板/项目", expected_path in spec, f"expect {expected_path!r} in spec")
    check("参数 image 已替换", "nginx:alpine" in spec, f"spec={spec!r}")
    check("应用归属租户已写入", (data(r) or {}).get("tenant_id") == TENANT_ID,
          f"tenant_id={(data(r) or {}).get('tenant_id')}")

# 幂等：再次实例化沿用同一 Application。
st, r = call("POST", f"/api/admin/projects/{pid}/instantiate",
             {"values": {"image": "nginx:alpine"}}, token=tok)
check("重复实例化幂等（同一 application_id）",
      st == 200 and (data(r) or {}).get("application_id") == app_id,
      f"http={st} app_id={(data(r) or {}).get('application_id')!r}")

# 负向：必填参数缺值。
st, r = call("POST", f"/api/admin/projects/{pid}/instantiate", {"values": {}}, token=tok)
check("必填参数缺值被拒绝", st == 400, f"http={st} msg={r.get('message','')[:60]}")

# 负向：number 参数传非数字。
num_tid = None
st, r = call("POST", "/api/admin/templates", {
    "name": "E2E Num", "slug": "e2e-tpl-num", "spec": "web:\n  image: {{image}}\n  port: {{port}}\n",
    "params": [
        {"key": "image", "label": "Image", "type": "string", "required": True},
        {"key": "port", "label": "Port", "type": "number", "default": "8080"},
    ],
}, token=tok)
num_tid = (data(r) or {}).get("id")
if num_tid:
    created_tpl.append(num_tid)
    st, r = call("POST", "/api/admin/projects", {
        "tenant_id": TENANT_ID, "template_id": num_tid, "name": "e2e-proj-num",
    }, token=tok)
    npid = (data(r) or {}).get("id")
    if npid:
        created_prj.append(npid)
        st, r = call("POST", f"/api/admin/projects/{npid}/instantiate",
                     {"values": {"image": "nginx", "port": "abc"}}, token=tok)
        check("number 参数非数字被拒绝", st == 400, f"http={st}")

# 负向：规格引用未声明参数。
undecl_tid = None
st, r = call("POST", "/api/admin/templates", {
    "name": "E2E Undecl", "slug": "e2e-tpl-undecl",
    "spec": "web:\n  image: {{image}}\n  x: {{ghost}}\n",
    "params": [{"key": "image", "label": "Image", "type": "string", "required": True}],
}, token=tok)
undecl_tid = (data(r) or {}).get("id")
if undecl_tid:
    created_tpl.append(undecl_tid)
    st, r = call("POST", "/api/admin/projects", {
        "tenant_id": TENANT_ID, "template_id": undecl_tid, "name": "e2e-proj-undecl",
    }, token=tok)
    upid = (data(r) or {}).get("id")
    if upid:
        created_prj.append(upid)
        st, r = call("POST", f"/api/admin/projects/{upid}/instantiate",
                     {"values": {"image": "nginx"}}, token=tok)
        check("规格引用未声明参数被拒绝", st == 400, f"http={st} msg={r.get('message','')[:60]}")

print("== 5. 节点数据目录字段校验 ==")
st, r = call("POST", "/api/admin/nodes", {
    "name": "e2e-node-a", "host": "127.0.0.1", "data_dir": DATA_DIR_OK,
}, token=tok)
nid = (data(r) or {}).get("id")
check("合法绝对数据目录被接受", st == 200 and (data(r) or {}).get("data_dir") == DATA_DIR_OK,
      f"http={st} data_dir={(data(r) or {}).get('data_dir')!r}")

st, r = call("POST", "/api/admin/nodes", {
    "name": "e2e-node-b", "host": "127.0.0.1", "data_dir": DATA_DIR_REL,
}, token=tok)
check("相对数据目录被拒绝", st == 400, f"http={st} msg={r.get('message','')[:50]}")

st, r = call("POST", "/api/admin/nodes", {
    "name": "e2e-node-c", "host": "127.0.0.1", "data_dir": DATA_DIR_TRAV,
}, token=tok)
check("含上跳段的数据目录被拒绝", st == 400, f"http={st} msg={r.get('message','')[:50]}")

# 编辑三态：设置 → 清空。
if nid:
    st, r = call("PATCH", f"/api/admin/nodes/{nid}", {"data_dir_clear": True}, token=tok)
    check("数据目录可显式清空", st == 200 and (data(r) or {}).get("data_dir") in ("", None),
          f"http={st} data_dir={(data(r) or {}).get('data_dir')!r}")
    # 清理该测试节点。
    call("DELETE", f"/api/admin/nodes/{nid}", token=tok)

print("== 6. 绑定挂载越界拒绝（版本规格）==")
# 用既有服务/部署挂一个临时版本，验证挂载校验。避免新建服务污染数据。
st, r = call("GET", "/api/admin/services?limit=1", token=tok)
svc = (data(r) or [{}])[0] if data(r) else None
if svc:
    st, r = call("GET", f"/api/admin/deployments?service_id={svc['id']}&limit=1", token=tok)
    deps = data(r) or []
    if deps:
        dep_id = deps[0]["id"]
        for mount, label, want_ok in [
            ({"path": "t1/webapp/p1/db", "target": "/data"}, "合法相对挂载被接受", True),
            ({"path": "../../etc", "target": "/etc"}, "上跳段挂载被拒绝", False),
            ({"path": "/abs/path", "target": "/etc"}, "绝对路径挂载被拒绝", False),
            ({"path": "t1/p", "target": "relative"}, "相对容器内路径被拒绝", False),
            ({"path": "t1/p", "target": "/x", "read_only": True}, "只读挂载被接受", True),
        ]:
            st, r = call("POST", f"/api/admin/deployments/{dep_id}/versions", {
                "version": f"e2e-mv-{abs(hash(json.dumps(mount))) % 100000}",
                "image": "nginx:alpine", "mounts": [mount],
            }, token=tok)
            got_ok = st == 200
            check(label, got_ok == want_ok, f"http={st} mount={mount}")
            # 清理成功创建的临时版本。
            vid = (data(r) or {}).get("id")
            if got_ok and vid:
                call("DELETE", f"/api/admin/versions/{vid}", token=tok)
    else:
        print("  [SKIP] 无可用部署，跳过挂载校验")
else:
    print("  [SKIP] 无可用服务，跳过挂载校验")

print("== 7. 清理 ==")
for a in created_app:
    st, r = call("DELETE", f"/api/admin/cm/applications/{a}", token=tok)
    check(f"清理应用 {a[:8]}", st == 200, f"http={st}")
for p in created_prj:
    st, r = call("DELETE", f"/api/admin/projects/{p}", token=tok)
    check(f"清理项目 {p[:8]}", st == 200, f"http={st}")
for t in created_tpl:
    st, r = call("DELETE", f"/api/admin/templates/{t}", token=tok)
    check(f"清理模板 {t[:8]}", st == 200, f"http={st}")

st, r = call("GET", "/api/admin/projects?limit=200", token=tok)
leftover_p = [p["name"] for p in (data(r) or []) if p["name"].startswith("e2e-proj")]
check("无残留 e2e-proj-* 项目", not leftover_p, f"leftover={leftover_p}")

st, r = call("GET", "/api/admin/templates?limit=200", token=tok)
leftover_t = [t["slug"] for t in (data(r) or []) if t["slug"].startswith("e2e-tpl")]
check("无残留 e2e-tpl-* 模板", not leftover_t, f"leftover={leftover_t}")

print(f"\n===== 结果：{len(PASS)} 通过 / {len(FAIL)} 失败 =====")
if FAIL:
    print("失败项：" + ", ".join(FAIL))
    sys.exit(1)
