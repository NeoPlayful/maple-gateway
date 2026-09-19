"""版本管理端到端验证：在 s2-svc/prod 下新增 v3 再删除，断言不波及同部署 v1/v2 的实例容器。

背景：一次「删除某版本却连带删掉同部署其它版本容器」的回归，源于把版本级删除
放大成部署级操作。本脚本复现完整链路并断言修复后的正确行为：

  1. 快照 v1/v2 版本与它们的实例容器（backend-v1/v2/v2b）。
  2. 新增 v3（带镜像与端口，触发 CM 建容器）。
  3. 等待 CM 建出 v3 容器并被观测。
  4. 删除 v3。
  5. 断言：
     - v1/v2 的容器原封不动（本次修复的核心目标）；
     - v3 的容器被回收、且不被对账器重建；
     - Gateway 侧版本集合回到 {v1, v2}；
     - CM 期望态不再指向被删的 v3（根因项）。

用法：python test/verify_version_delete.py
依赖运行中的 Gateway(8090) / CM(9091) / NodeAgent / Docker，且 s2-svc/prod 已存在。
脚本只新增并删除自己创建的 v3；不触碰 backend-v1/v2/v2b 等既有测试容器。
"""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:8090"
EMAIL, PASSWORD = "admin@maple.com", "admin123"
SVC_NAME = "s2-svc"
DEP_NAME = "prod"
V3_IMAGE = "nginx:alpine"
V3_PORT = 8080

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


def login():
    _, r = call("POST", "/api/auth/login", {"email": EMAIL, "password": PASSWORD})
    return (data(r) or {}).get("token")


def find_service(tok, name):
    _, r = call("GET", "/api/admin/services?limit=200", token=tok)
    for s in data(r) or []:
        if s["name"] == name:
            return s
    return None


def find_deployment(tok, service_id, name):
    _, r = call("GET", f"/api/admin/deployments?service_id={service_id}&limit=200", token=tok)
    for d in data(r) or []:
        if d["name"] == name:
            return d
    return None


def list_versions(tok, dep_id):
    _, r = call("GET", f"/api/admin/deployments/{dep_id}/versions", token=tok)
    return data(r) or []


def list_containers(tok):
    """经 CM 聚合代理取受管容器清单（含 maple 标签）。"""
    _, r = call("GET", "/api/admin/cm/containers", token=tok)
    return data(r) or []


def containers_of_version(tok, version_id):
    out = []
    for c in list_containers(tok):
        if (c.get("labels") or {}).get("maple.version_id") == version_id:
            out.append(c)
    return out


def desired_state(dep_id):
    """诊断用：直查 CM 期望态表。返回 (raw, active_version_id)：
    raw 为整行字符串；active_version_id 仅当期望态处于「生效」状态（行存在且 stopped=false）
    时为其指向的 version_id，否则为 None（视为对账器不会再据它补建容器）。"""
    sql = ("SELECT version||'|'||version_id||'|stopped='||stopped||'|phase='||phase "
           f"FROM cm_deployments WHERE deployment_id='{dep_id}';")
    try:
        p = subprocess.run(
            ["docker", "exec", "postgres", "psql", "-U", "maple", "-d", "maple", "-tA", "-c", sql],
            capture_output=True, text=True, timeout=20)
        if p.returncode != 0:
            return None, None
        raw = p.stdout.strip()
        if not raw:
            return raw, None
        # 行格式：version|version_id|stopped=true|phase=ready
        parts = raw.split("|")
        vid = parts[1] if len(parts) > 1 else ""
        stopped = any(x == "stopped=true" for x in parts)
        return raw, (None if stopped else vid)
    except Exception:
        return None, None


def main():
    print("== 0. 登录与定位 ==")
    tok = login()
    if not tok:
        print("登录失败，终止。")
        sys.exit(1)
    svc = find_service(tok, SVC_NAME)
    if not svc:
        print(f"未找到服务 {SVC_NAME}，终止。")
        sys.exit(1)
    dep = find_deployment(tok, svc["id"], DEP_NAME)
    if not dep:
        print(f"未找到部署 {DEP_NAME}@{SVC_NAME}，终止。")
        sys.exit(1)
    dep_id = dep["id"]
    print(f"目标：{SVC_NAME} / {DEP_NAME} ({dep_id})\n")

    # ---- 1. 基线 ----
    print("== 1. 基线快照 ==")
    versions = list_versions(tok, dep_id)
    vids = {v["version"]: v["id"] for v in versions}
    print(f"  现有版本：{[v['version'] for v in versions]}")
    if "v3" in vids:
        print("  ! 已存在 v3，请先手动删除后再跑本脚本。")
        sys.exit(1)
    base = {}
    for label in ("v1", "v2"):
        if label in vids:
            base[label] = {c["instance_id"] for c in containers_of_version(tok, vids[label])}
            print(f"  {label}: {len(base[label])} 个容器 {sorted(x[:8] for x in base[label])}")
    print(f"  CM 期望态：{desired_state(dep_id)[0]}\n")

    # ---- 2. 新增 v3 ----
    print("== 2. 新增 v3 ==")
    st, r = call("POST", f"/api/admin/deployments/{dep_id}/versions",
                 {"version": "v3", "image": V3_IMAGE, "replicas": 1, "port": V3_PORT,
                  "status": "standby"}, token=tok)
    v3_id = (data(r) or {}).get("id")
    check("创建 v3 成功", st == 200 and bool(v3_id), f"http={st} id={v3_id}")
    if not v3_id:
        sys.exit(1)

    # ---- 3. 等 CM 建出 v3 容器并被观测 ----
    print("== 3. 等待 CM 建出并观测 v3 容器 ==")
    got = 0
    for _ in range(20):
        got = len(containers_of_version(tok, v3_id))
        if got > 0:
            break
        time.sleep(2)
    check("CM 为 v3 建出容器并被观测", got > 0, f"count={got}")
    print(f"  CM 期望态：{desired_state(dep_id)[0]}\n")

    # ---- 4. 删除 v3 ----
    print("== 4. 删除 v3 ==")
    st, r = call("DELETE", f"/api/admin/versions/{v3_id}", token=tok)
    check("删除 v3 成功", st == 200, f"http={st} msg={r.get('message', '')}")
    print("  等待对账收敛（最多 45s）...")
    deadline = time.time() + 45
    while time.time() < deadline:
        time.sleep(5)
        if len(containers_of_version(tok, v3_id)) == 0:
            break

    # ---- 5. 断言 ----
    print("== 5. 断言 ==")
    # 5.1 v1/v2 容器原封不动
    for label in ("v1", "v2"):
        now = {c["instance_id"] for c in containers_of_version(tok, vids[label])}
        check(f"{label} 的容器原封不动", now == base.get(label, set()),
              f"before={len(base.get(label, set()))} after={len(now)}")

    # 5.2 v3 容器被回收
    residual = containers_of_version(tok, v3_id)
    check("v3 容器被回收（0 残留）", len(residual) == 0,
          f"仍残留 {len(residual)} 个: {[c['name'] for c in residual]}")

    # 5.3 Gateway 版本集合回到 {v1, v2}
    after_versions = {v["version"] for v in list_versions(tok, dep_id)}
    check("Gateway 版本集合回到 {v1, v2}", after_versions == {"v1", "v2"}, f"{after_versions}")

    # 5.4 CM 期望态不再对被删的 v3「生效」（根因项）：
    #     要么无行，要么该行已 stopped=true。只要生效期望态仍指向 v3，对账器就会重建它。
    ds_raw, active_vid = desired_state(dep_id)
    print(f"  CM 期望态：{ds_raw}  (生效指向={active_vid})")
    check("CM 期望态不再对 v3 生效", active_vid != v3_id,
          f"active_version_id={active_vid} v3_id={v3_id}")

    print(f"\n===== 结果：{len(PASS)} 通过 / {len(FAIL)} 失败 =====")
    if FAIL:
        print("失败项：" + ", ".join(FAIL))
        sys.exit(1)


if __name__ == "__main__":
    main()
