"""Compose 容器纳入实例管理：端到端验证派生实例 ID 及其可操作性。

背景：Compose 应用建出的容器（maple-<short>-<svc>-<idx>）原先无 instance_id 标签，
被观测器整体跳过，既不进实例视图也不能按容器操作。修复后观测器以「容器名」派生稳定
实例 ID（UUIDv5），使每个容器像版本单容器一样进入实例管理，且派生 ID 可被运行时页用来
采集指标/读日志/启停（否则按该 ID 定位不到容器，报 502）。

本脚本覆盖：
  1. 派生 ID 的确定性：由容器名算出的 UUIDv5 与后端上报的 instance_id 逐字一致；
  2. 派生 ID 可用于逐容器操作（指标/日志）——即本次修复的 502 目标；
  3. 未绑定服务：容器仍可被定位操作，但不注册为实例（不进实例列表）；
  4. 绑定服务：容器注册为实例，且 service_id 正确落到实例上；
  5. 稳定性：同一容器跨轮次 instance_id 不变。

自建自清：仅用 e2e-inst-* 命名，跑完 compose down + 删应用，不触碰既有测试数据。
用法：python test/verify_compose_instance.py
依赖运行中的 Gateway(8090) / CM(9091) / NodeAgent / Docker。
"""
import json
import sys
import time
import urllib.error
import urllib.request
import uuid

BASE = "http://127.0.0.1:8090"
EMAIL, PASSWORD = "admin@maple.com", "admin123"

# 与 observer.instanceIDNamespace 一致的 UUIDv5 命名空间（派生实例 ID 的固定常量）。
NS = uuid.UUID("6f6f6b1e-9c2a-4b7e-9f1a-2b3c4d5e6f70")

SRC_PORT = 18120  # 本脚本占用端口，避开既有测试数据

SPEC = """services:
  web:
    image: nginx:alpine
    ports:
      - "%d:80"
""" % SRC_PORT

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


def short_id(app_id):
    """与 pkg.ShortID 一致：去连字符后取前 12 位。"""
    return app_id.replace("-", "")[:12]


def find_app(tok, name):
    _, r = call("GET", "/api/admin/cm/applications", token=tok)
    for a in data(r) or []:
        if a["name"] == name:
            return a
    return None


def containers(tok):
    _, r = call("GET", "/api/admin/cm/containers", token=tok)
    return data(r) or []


def container_named(tok, name):
    for c in containers(tok):
        if c.get("name") == name:
            return c
    return None


def instances(tok):
    _, r = call("GET", "/api/admin/instances?limit=500", token=tok)
    return data(r) or []


def instance_by_id(tok, iid):
    for i in instances(tok):
        if i["id"] == iid:
            return i
    return None


def wait_container(tok, name, timeout=90):
    """等观测器发现新容器（观测周期 + 事件触发，通常数秒内）。"""
    deadline = time.time() + timeout
    while time.time() < deadline:
        c = container_named(tok, name)
        if c:
            return c
        time.sleep(2)
    return None


def wait_stat(tok, iid, want_ok, timeout=90):
    """轮询 stats，直到达到期望的可采集/不可采集状态。返回最后一次 (status, body)。"""
    deadline = time.time() + timeout
    last = (None, None)
    while time.time() < deadline:
        st, r = call("GET", f"/api/admin/cm/instances/{iid}/stats", token=tok)
        last = (st, r)
        if want_ok and st == 200:
            return last
        if not want_ok and st != 200:
            return last
        time.sleep(2)
    return last


def main():
    print("== 0. 登录 ==")
    tok = login()
    if not tok:
        print("登录失败，终止。")
        sys.exit(1)

    # 定位一个可用服务（用于「绑定服务」用例）。
    _, r = call("GET", "/api/admin/services?limit=50", token=tok)
    services = data(r) or []
    if not services:
        print("无可用服务，终止（绑定服务用例需要至少一个服务）。")
        sys.exit(1)
    svc_id = services[0]["id"]
    svc_name = services[0]["name"]
    print(f"  绑定服务：{svc_name} ({svc_id})\n")

    created = []  # 收尾清理用

    try:
        # ---- 1. 未绑定服务：容器可操作，但不进实例视图 ----
        print("== 1. 未绑定服务的 Compose 应用 ==")
        name_unbound = "e2e-inst-unbound"
        st, r = call("POST", "/api/admin/cm/applications",
                     {"name": name_unbound, "description": "e2e 未绑定服务", "version": "v1",
                      "spec": SPEC}, token=tok)
        app = find_app(tok, name_unbound)
        check("创建未绑定应用", st == 200 and bool(app), f"http={st}")
        if not app:
            sys.exit(1)
        created.append(app["id"])
        cname_unbound = f"maple-{short_id(app['id'])}-web-1"

        st, r = call("POST", f"/api/admin/cm/applications/{app['id']}/deploy", token=tok)
        check("部署未绑定应用", st == 200, f"http={st} msg={r.get('message','')}")

        c = wait_container(tok, cname_unbound)
        check("观测到容器 " + cname_unbound, c is not None, f"name={cname_unbound}")
        if not c:
            sys.exit(1)

        want_iid = str(uuid.uuid5(NS, cname_unbound))
        got_iid = c.get("instance_id") or ""
        check("派生的 instance_id = UUIDv5(容器名)", got_iid == want_iid,
              f"got={got_iid} want={want_iid}")
        check("派生 ID 版本号为 5（UUIDv5）",
              len(got_iid) == 36 and got_iid[14] == "5", f"iid={got_iid}")

        # 本次修复核心：派生 ID 可定位容器 → 指标/日志可用（否则 502）。
        st, r = wait_stat(tok, got_iid, want_ok=True)
        cid = (data(r) or {}).get("container_id") if st == 200 else None
        check("按派生 ID 采集容器指标（修复 502）", st == 200 and bool(cid),
              f"http={st} container_id={(cid or '')[:12]}")
        st, r = call("GET", f"/api/admin/cm/instances/{got_iid}/logs?tail=5", token=tok)
        check("按派生 ID 读取容器日志", st == 200 and "logs" in (data(r) or {}),
              f"http={st}")

        # 未绑定服务：不进实例列表（service_id 必填，注册会被拒）。
        reg = instance_by_id(tok, got_iid)
        check("未绑定服务的容器不注册为实例", reg is None,
              f"instances 中 {'存在' if reg else '不存在'}")

        # ---- 2. 绑定服务：容器注册为实例，service_id 正确 ----
        print("\n== 2. 绑定服务的 Compose 应用 ==")
        name_bound = "e2e-inst-bound"
        st, r = call("POST", "/api/admin/cm/applications",
                     {"name": name_bound, "description": "e2e 绑定服务", "version": "v1",
                      "spec": SPEC.replace(str(SRC_PORT), str(SRC_PORT + 1)),
                      "service_id": svc_id}, token=tok)
        app_b = find_app(tok, name_bound)
        check("创建绑定应用且回读 service_id", st == 200 and bool(app_b) and app_b.get("service_id") == svc_id,
              f"http={st} service_id={(app_b or {}).get('service_id')}")
        if not app_b:
            sys.exit(1)
        created.append(app_b["id"])
        cname_bound = f"maple-{short_id(app_b['id'])}-web-1"

        st, r = call("POST", f"/api/admin/cm/applications/{app_b['id']}/deploy", token=tok)
        check("部署绑定应用", st == 200, f"http={st} msg={r.get('message','')}")

        c = wait_container(tok, cname_bound)
        check("观测到容器 " + cname_bound, c is not None)
        if not c:
            sys.exit(1)
        want_iid_b = str(uuid.uuid5(NS, cname_bound))
        got_iid_b = c.get("instance_id") or ""
        check("绑定容器派生 ID 正确", got_iid_b == want_iid_b,
              f"got={got_iid_b} want={want_iid_b}")

        # 观测器注册实例需数秒；轮询直到出现。
        reg = None
        deadline = time.time() + 90
        while time.time() < deadline:
            reg = instance_by_id(tok, got_iid_b)
            if reg:
                break
            time.sleep(3)
        check("绑定服务的容器注册为实例", reg is not None,
              f"iid={got_iid_b[:8]} present={reg is not None}")
        if reg:
            check("实例 service_id 指向绑定服务", reg.get("service_id") == svc_id,
                  f"got={reg.get('service_id')} want={svc_id}")
        st, r = call("GET", f"/api/admin/cm/instances/{got_iid_b}/stats", token=tok)
        check("绑定容器亦可采集指标", st == 200, f"http={st}")

        # ---- 3. 稳定性：跨轮次 instance_id 不变 ----
        print("\n== 3. 派生 ID 稳定性 ==")
        time.sleep(6)  # 跨过一个观测周期
        c2 = container_named(tok, cname_unbound)
        check("跨轮次 instance_id 不变",
              bool(c2) and c2.get("instance_id") == got_iid,
              f"before={got_iid[:8]} after={(c2 or {}).get('instance_id','-')[:8]}")

    finally:
        # ---- 收尾：compose down + 删应用（仅本脚本创建的） ----
        print("\n== 收尾 ==")
        prefixes = tuple(f"maple-{short_id(aid)}" for aid in created)
        for aid in created:
            st, r = call("DELETE", f"/api/admin/cm/applications/{aid}", token=tok)
            print(f"  删除应用 {aid[:8]}: http={st} {r.get('message','')}")
        time.sleep(10)
        names = [c.get("name") for c in containers(tok)]
        leftover = [n for n in names if n and n.startswith(prefixes)]
        check("无残留 e2e 容器", not leftover, f"leftover={leftover}")

    print(f"\n===== 结果：{len(PASS)} 通过 / {len(FAIL)} 失败 =====")
    if FAIL:
        print("失败项：" + ", ".join(FAIL))
        sys.exit(1)


if __name__ == "__main__":
    main()
