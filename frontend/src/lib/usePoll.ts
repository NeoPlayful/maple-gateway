// 轮询 hook：按固定间隔反复取数，直到 shouldStop 判定终态或 key 变化/置空。
// 用 key 控制生命周期（key 为 null 即停止并清空），fetcher/shouldStop 走 ref 保持最新。
import { useEffect, useRef, useState } from 'react';

export function usePoll<T>(
  key: string | null,
  fetcher: () => Promise<T>,
  shouldStop: (value: T) => boolean,
  intervalMs = 1500,
): T | null {
  const [value, setValue] = useState<T | null>(null);
  const fetcherRef = useRef(fetcher);
  const stopRef = useRef(shouldStop);
  fetcherRef.current = fetcher;
  stopRef.current = shouldStop;

  useEffect(() => {
    if (!key) {
      setValue(null);
      return;
    }
    // 切换 key（新一次操作）时清空旧值，避免闪现上一次的终态。
    setValue(null);
    let cancelled = false;
    let timer: number | undefined;
    const tick = async () => {
      try {
        const v = await fetcherRef.current();
        if (cancelled) return;
        setValue(v);
        if (stopRef.current(v)) return; // 终态，停止轮询
      } catch {
        // 轮询失败继续重试，不中断
      }
      if (!cancelled) timer = window.setTimeout(tick, intervalMs);
    };
    tick();
    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [key, intervalMs]);

  return value;
}
