"""Maple Gateway 数据平面快速压测（冒烟）。

用法: python bench.py <host> <url> <concurrency> <total>
"""
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor


def main():
    host = sys.argv[1]
    url = sys.argv[2]
    concurrency = int(sys.argv[3]) if len(sys.argv) > 3 else 20
    total = int(sys.argv[4]) if len(sys.argv) > 4 else 500

    start = time.time()
    errs = 0
    lat = []

    def one(_):
        nonlocal errs
        t0 = time.time()
        try:
            req = urllib.request.Request(url, headers={"Host": host})
            with urllib.request.urlopen(req, timeout=5) as resp:
                resp.read()
                status = resp.status
            if status >= 400:
                errs += 1
        except Exception:
            errs += 1
        lat.append((time.time() - t0) * 1000)

    with ThreadPoolExecutor(max_workers=concurrency) as ex:
        list(ex.map(one, range(total)))

    dur = time.time() - start
    rps = total / dur if dur else 0
    lat.sort()
    p50 = lat[len(lat) // 2]
    p99 = lat[int(len(lat) * 0.99) - 1]
    print("total=%d errs=%d duration=%.2fs rps=%.0f p50=%.1fms p99=%.1fms" %
          (total, errs, dur, rps, p50, p99))


if __name__ == "__main__":
    main()
