"""网关数据平面压测 + 进程资源采样（定位崩溃原因）。

用多线程 HTTP 客户端对网关数据面（默认 :8091）施加递增并发，同时用 psutil 采样
网关进程的线程数/句柄数/内存，观察是否触发 Go 运行时线程耗尽而崩溃。

用法：
  python test/bench_gateway.py                      # 默认阶梯压测
  python test/bench_gateway.py --host shop-a.test --url http://127.0.0.1:8091/health
  python test/bench_gateway.py --concurrency 200 --total 40000
"""
import argparse
import http.client
import sys
import threading
import time

try:
    import psutil
except ImportError:
    psutil = None


def find_gateway_procs():
    """定位监听 :8091 的网关进程（数据平面）。"""
    procs = []
    if psutil is None:
        return procs
    api = getattr(psutil.Process, "net_connections", None) or \
        getattr(psutil.Process, "connections")
    for p in psutil.process_iter(["pid", "name"]):
        try:
            for c in api(p, kind="tcp"):
                if c.status == psutil.CONN_LISTEN and c.laddr and c.laddr.port == 8091:
                    procs.append(p)
                    break
        except (psutil.NoSuchProcess, psutil.AccessDenied):
            continue
    return procs


class Sampler(threading.Thread):
    """后台采样网关进程资源，记录峰值与是否进程消失。"""

    def __init__(self, proc, interval=0.5):
        super().__init__(daemon=True)
        self.proc = proc
        self.interval = interval
        self.peak_threads = 0
        self.peak_handles = 0
        self.peak_mem = 0.0
        self.samples = []
        self.died = False
        self._stop = threading.Event()

    def run(self):
        while not self._stop.is_set():
            try:
                t = self.proc.num_threads()
                h = self.proc.num_handles() if hasattr(self.proc, "num_handles") else 0
                m = self.proc.memory_info().rss / (1024 * 1024)
                self.peak_threads = max(self.peak_threads, t)
                self.peak_handles = max(self.peak_handles, h)
                self.peak_mem = max(self.peak_mem, m)
                self.samples.append((round(time.time(), 2), t, h, round(m, 1)))
            except psutil.NoSuchProcess:
                self.died = True
                break
            except Exception:
                pass
            self._stop.wait(self.interval)

    def stop(self):
        self._stop.set()


def one_request(host, url, timeout):
    """单请求：用短连接 http.client，模拟不复用的上游/客户端行为。"""
    from urllib.parse import urlparse
    u = urlparse(url)
    conn = http.client.HTTPConnection(u.hostname, u.port, timeout=timeout)
    try:
        path = u.path or "/"
        if u.query:
            path += "?" + u.query
        conn.request("GET", path, headers={"Host": host, "Connection": "close"})
        resp = conn.getresponse()
        resp.read()
        return resp.status < 400
    finally:
        conn.close()


def run_wave(host, url, concurrency, total, timeout):
    """一次并发波次：固定并发 worker，跑完 total 个请求。"""
    lock = threading.Lock()
    idx = [0]
    ok = [0]
    errs = [0]
    lats = []

    def worker():
        while True:
            with lock:
                if idx[0] >= total:
                    return
                idx[0] += 1
            t0 = time.time()
            try:
                if one_request(host, url, timeout):
                    with lock:
                        ok[0] += 1
                else:
                    with lock:
                        errs[0] += 1
            except Exception:
                with lock:
                    errs[0] += 1
            with lock:
                lats.append((time.time() - t0) * 1000)

    start = time.time()
    ts = [threading.Thread(target=worker) for _ in range(concurrency)]
    for t in ts:
        t.start()
    for t in ts:
        t.join()
    dur = time.time() - start
    return ok[0], errs[0], dur, lats, idx[0]


def run_wave_asyncio(host, url, concurrency, total, timeout):
    """asyncio 波次：用信号量把在途请求稳定压在 concurrency 个，
    以制造网关侧大量同时存在的上游连接（触发连接/线程扩张）。"""
    import asyncio
    from urllib.parse import urlparse

    u = urlparse(url)
    path = u.path or "/"
    if u.query:
        path += "?" + u.query
    hostname = u.hostname
    port = u.port
    ok = [0]
    errs = [0]
    lats = []
    idx = [0]

    async def one():
        t0 = time.time()
        w = None
        try:
            r, w = await asyncio.wait_for(
                asyncio.open_connection(hostname, port), timeout)
            req = (f"GET {path} HTTP/1.1\r\nHost: {host}\r\n"
                   f"Connection: close\r\n\r\n").encode()
            w.write(req)
            await asyncio.wait_for(w.drain(), timeout)
            head = await asyncio.wait_for(r.readuntil(b"\r\n\r\n"), timeout)
            first = head.split(b"\r\n", 1)[0].split()
            ok[0] += 1 if len(first) >= 2 and first[1].startswith(b"2") else 0
            if not (len(first) >= 2 and first[1].startswith(b"2")):
                errs[0] += 1
        except Exception:
            errs[0] += 1
        finally:
            if w is not None:
                try:
                    w.close()
                except Exception:
                    pass
            lats.append((time.time() - t0) * 1000)

    async def drive():
        sem = asyncio.Semaphore(concurrency)

        async def task():
            async with sem:
                with_lock = idx[0]
                idx[0] += 1
                _ = with_lock
                await one()

        tasks = [asyncio.create_task(task()) for _ in range(total)]
        await asyncio.gather(*tasks)

    start = time.time()
    asyncio.run(drive())
    dur = time.time() - start
    return ok[0], errs[0], dur, lats, idx[0]



def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="shop-a.test")
    ap.add_argument("--url", default="http://127.0.0.1:8091/health")
    ap.add_argument("--steps", default="20,50,100,150,200",
                    help="并发阶梯（逗号分隔）")
    ap.add_argument("--total", type=int, default=8000, help="每波请求数")
    ap.add_argument("--timeout", type=float, default=5.0)
    ap.add_argument("--engine", choices=["thread", "asyncio"], default="thread",
                    help="thread=多线程；asyncio=高在途连接（更能压出连接扩张）")
    args = ap.parse_args()

    procs = find_gateway_procs()
    if not procs:
        print("未找到监听 :8091 的网关进程，终止。")
        sys.exit(1)
    proc = procs[0]
    print(f"网关进程: pid={proc.pid} name={proc.name()}")
    print(f"目标: Host={args.host} URL={args.url}\n")

    sampler = Sampler(proc)
    sampler.start()

    try:
        for c in [int(x) for x in args.steps.split(",")]:
            runner = run_wave_asyncio if args.engine == "asyncio" else run_wave
            ok, errs, dur, lats, done = runner(
                args.host, args.url, c, args.total, args.timeout)
            rps = done / dur if dur else 0
            lats.sort()
            p50 = lats[len(lats) // 2] if lats else 0
            p99 = lats[int(len(lats) * 0.99) - 1] if lats else 0
            peak_t = sampler.peak_threads
            cur_t = proc.num_threads() if not sampler.died else -1
            print(f"c={c:4d} total={done:6d} ok={ok:6d} errs={errs:6d} "
                  f"dur={dur:6.2f}s rps={rps:7.0f} p50={p50:7.1f}ms p99={p99:7.1f}ms "
                  f"| threads peak={peak_t} now={cur_t} handles_peak={sampler.peak_handles}")
            if sampler.died:
                print("\n!! 网关进程已消失（崩溃）—— 终止压测。")
                break
    finally:
        sampler.stop()
        time.sleep(0.6)

    print("\n=== 采样摘要 ===")
    print(f"线程峰值   : {sampler.peak_threads}")
    print(f"句柄峰值   : {sampler.peak_handles}")
    print(f"内存峰值   : {sampler.peak_mem:.1f} MB")
    print(f"进程是否崩 : {'是' if sampler.died else '否'}")
    if sampler.samples:
        print("轨迹(时间,线程,句柄,MB)：")
        step = max(1, len(sampler.samples) // 25)
        for s in sampler.samples[::step]:
            print(f"  {s}")

    if sampler.died:
        sys.exit(2)


if __name__ == "__main__":
    main()
