import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, ApiError, getToken } from '../../lib/client';
import { list } from '../../lib/modules';
import type { CMOverview, CMNodeStatus, NodeMetric, CMRuntimeError, CMContainer, CMDockerEvent, CMTask, Instance } from '../../types';
import { PageHeader } from '../../themes';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { Modal } from '../../components/admin/Modal';
import { Pagination } from '../../components/admin/Pagination';

// 子组件共用的翻译函数签名（来自 useTranslation('admin')）。
type TFn = (key: string, opts?: Record<string, unknown>) => string;

// 把字节数格式化为易读单位。
function fmtBytes(n: number): string {
  if (!n || n <= 0) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

// 把运行秒数格式化为「N天N时N分」。
function fmtUptime(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

// 使用率配色：越高越接近告警。
function usageColor(pct: number): string {
  if (pct >= 90) return 'bg-rose-500';
  if (pct >= 75) return 'bg-amber-500';
  return 'bg-emerald-500';
}

function UsageBar({ label, pct }: { label: string; pct: number }) {
  return (
    <div className="flex items-center gap-2">
      {label && <span className="shrink-0 text-xs text-slate-500 dark:text-slate-400">{label}</span>}
      <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
        <div className={`h-full rounded-full ${usageColor(pct)}`} style={{ width: `${Math.min(pct, 100)}%` }} />
      </div>
      <span className="w-12 shrink-0 text-right font-mono text-xs text-slate-500 dark:text-slate-400">{pct.toFixed(1)}%</span>
    </div>
  );
}

// 节点详情内「最近下发任务」内联小表：分页展示，每页 PAGE_SIZE 条。
const TASK_PAGE_SIZE = 10;

function NodeTasks({
  tasks, page, onPage, t, onOpen, onRetry, onCancel,
}: {
  tasks: CMTask[];
  page: number;
  onPage: (page: number) => void;
  t: TFn;
  onOpen: (id: string) => void;
  onRetry: (id: string) => void;
  onCancel: (id: string) => void;
}) {
  const totalPages = Math.max(1, Math.ceil(tasks.length / TASK_PAGE_SIZE));
  const cur = Math.min(Math.max(page, 1), totalPages);
  const shown = tasks.slice((cur - 1) * TASK_PAGE_SIZE, cur * TASK_PAGE_SIZE);
  return (
    <div className="mt-3">
      <p className="mb-1 text-xs font-semibold text-slate-500 dark:text-slate-400">{t('runtime.tasks')}</p>
      <div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700">
        <table className="w-full text-xs">
          <thead className="border-b border-slate-200 bg-slate-100 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400">
            <tr>
              <th className="px-3 py-1.5">{t('runtime.colTaskId')}</th>
              <th className="px-3 py-1.5">{t('runtime.colAction')}</th>
              <th className="px-3 py-1.5">{t('runtime.colStatus')}</th>
              <th className="px-3 py-1.5">{t('runtime.colProgress')}</th>
              <th className="px-3 py-1.5">{t('runtime.colCreatedAt')}</th>
              <th className="px-3 py-1.5">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {tasks.length === 0 ? (
              <tr><td colSpan={6} className="px-3 py-4 text-center text-slate-400 dark:text-slate-500">{t('runtime.noTasks')}</td></tr>
            ) : shown.map((tk) => (
              <tr key={tk.id} className="border-b border-slate-200 last:border-b-0 dark:border-slate-700">
                <td className="px-3 py-1.5 font-mono">
                  <button onClick={() => onOpen(tk.id)} className="text-sky-600 hover:underline dark:text-sky-400">{tk.id.slice(0, 12)}</button>
                </td>
                <td className="px-3 py-1.5 font-mono">{tk.action}</td>
                <td className="px-3 py-1.5"><StatusBadge value={tk.status} raw /></td>
                <td className="px-3 py-1.5 text-slate-500">
                  {tk.percent !== undefined && tk.percent > 0 ? `${tk.percent}%` : '-'}
                  {!!tk.attempts && tk.attempts > 1 && <span className="ml-1 text-slate-400">×{tk.attempts}</span>}
                </td>
                <td className="px-3 py-1.5 text-slate-400">{tk.created_at_ms ? new Date(tk.created_at_ms).toLocaleString() : '-'}</td>
                <td className="px-3 py-1.5">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => onOpen(tk.id)}>{t('runtime.taskDetail')}</ActionBtn>
                    {['failed', 'timeout', 'cancelled'].includes(tk.status) && (
                      <ActionBtn onClick={() => onRetry(tk.id)}>{t('runtime.retry')}</ActionBtn>
                    )}
                    {['pending', 'dispatching', 'running'].includes(tk.status) && (
                      <ActionBtn onClick={() => onCancel(tk.id)}>{t('common.cancel')}</ActionBtn>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <Pagination page={cur} pageSize={TASK_PAGE_SIZE} total={tasks.length} onChange={onPage} />
    </div>
  );
}

// 节点详情内「最近容器事件」内联小表：默认显示最近 10 条，超出可展开。
const EVENT_INLINE_LIMIT = 10;

function NodeEvents({
  events, expanded, onToggle, t,
}: {
  events: CMDockerEvent[];
  expanded: boolean;
  onToggle: () => void;
  t: TFn;
}) {
  const shown = expanded ? events : events.slice(0, EVENT_INLINE_LIMIT);
  return (
    <div className="mt-3">
      <p className="mb-1 text-xs font-semibold text-slate-500 dark:text-slate-400">{t('runtime.events')}</p>
      <div className="overflow-x-auto rounded-lg border border-slate-200 dark:border-slate-700">
        <table className="w-full text-xs">
          <thead className="border-b border-slate-200 bg-slate-100 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-400">
            <tr>
              <th className="px-3 py-1.5">{t('runtime.colTime')}</th>
              <th className="px-3 py-1.5">{t('runtime.colEventAction')}</th>
              <th className="px-3 py-1.5">{t('runtime.colInstance')}</th>
              <th className="px-3 py-1.5">{t('runtime.colImage')}</th>
              <th className="px-3 py-1.5">{t('runtime.colExitCode')}</th>
            </tr>
          </thead>
          <tbody>
            {events.length === 0 ? (
              <tr><td colSpan={5} className="px-3 py-4 text-center text-slate-400 dark:text-slate-500">{t('runtime.noEvents')}</td></tr>
            ) : shown.map((e, i) => (
              <tr key={`${e.container_id}-${e.action}-${e.at}-${i}`} className="border-b border-slate-200 last:border-b-0 dark:border-slate-700">
                <td className="px-3 py-1.5 text-slate-400">{e.at ? new Date(e.at * 1000).toLocaleString() : '-'}</td>
                <td className="px-3 py-1.5">
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-slate-700 dark:bg-slate-700 dark:text-slate-200">{e.action}</span>
                </td>
                <td className="px-3 py-1.5 font-mono">{(e.instance_id || e.container_id || '').slice(0, 12) || '-'}</td>
                <td className="px-3 py-1.5 font-mono">{e.image || '-'}</td>
                <td className="px-3 py-1.5 font-mono">{e.exit_code ?? '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {events.length > EVENT_INLINE_LIMIT && (
        <button onClick={onToggle} className="mt-1 text-xs text-sky-600 hover:underline dark:text-sky-400">
          {expanded ? t('common.collapse') : t('runtime.moreCount', { count: events.length - EVENT_INLINE_LIMIT })}
        </button>
      )}
    </div>
  );
}

export default function RuntimePage() {
  const { t } = useTranslation('admin');
  const [overview, setOverview] = useState<CMOverview | null>(null);
  const [metrics, setMetrics] = useState<Record<string, NodeMetric>>({});
  const [errors, setErrors] = useState<CMRuntimeError[]>([]);
  const [containers, setContainers] = useState<CMContainer[]>([]);
  const [events, setEvents] = useState<CMDockerEvent[]>([]);
  const [tasks, setTasks] = useState<CMTask[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [last, setLast] = useState('');
  const [log, setLog] = useState<{ id: string; text: string } | null>(null);
  const [taskDetail, setTaskDetail] = useState<CMTask | null>(null);
  const [following, setFollowing] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const preRef = useRef<HTMLPreElement>(null);
  // 展开的节点名集合：独立于轮询数据，10s 刷新不会收起已展开的详情。
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  // 详情内任务表分页（按节点名记录当前页，1-based）；事件表仍用「展开更多」。
  const [taskPages, setTaskPages] = useState<Record<string, number>>({});
  const [moreEvents, setMoreEvents] = useState<Set<string>>(new Set());
  const toggleSet = (setter: React.Dispatch<React.SetStateAction<Set<string>>>, name: string) =>
    setter((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  const toggleNode = (name: string) => toggleSet(setExpanded, name);

  const load = useCallback(async () => {
    try {
      const [ov, mt, er, ct, ev, tk, ins] = await Promise.all([
        api.get<CMOverview>('/api/admin/cm/overview'),
        api.get<Record<string, NodeMetric>>('/api/admin/cm/metrics'),
        api.get<CMRuntimeError[]>('/api/admin/cm/errors'),
        api.get<CMContainer[]>('/api/admin/cm/containers'),
        api.get<CMDockerEvent[]>('/api/admin/cm/events'),
        api.get<CMTask[]>('/api/admin/cm/tasks').catch(() => [] as CMTask[]),
        list<Instance>('/api/admin/instances').catch(() => [] as Instance[]),
      ]);
      setOverview(ov);
      setMetrics(mt ?? {});
      setErrors(er ?? []);
      setContainers(ct ?? []);
      setEvents(ev ?? []);
      setTasks(tk ?? []);
      // Gateway 实例列表：用 instance_id 关联出 服务/版本，补全容器行。
      setInstances(ins ?? []);
      setEnabled(true);
      setErr('');
      setLast(new Date().toLocaleTimeString());
    } catch (e) {
      if (e instanceof ApiError && e.status === 503) {
        setEnabled(false);
        setErr('');
      } else {
        setErr(e instanceof Error ? e.message : t('common.loadFailed'));
      }
    }
  }, [t]);

  // 容器操作：启/停/重启，复用 CM 人工控制端点。
  const act = async (id: string, a: string) => {
    try {
      await api.post(`/api/admin/cm/instances/${id}/${a}`);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  // 任务操作：重试（终态）/取消（未结束）。
  const retryTask = async (id: string) => {
    try {
      await api.post(`/api/admin/cm/tasks/${id}/retry`);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const cancelTask = async (id: string) => {
    try {
      await api.post(`/api/admin/cm/tasks/${id}/cancel`, { reason: 'operator' });
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const openTask = async (id: string) => {
    try {
      const d = await api.get<CMTask>(`/api/admin/cm/tasks/${id}`);
      setTaskDetail(d);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  };

  // 停止跟随（中止流读取）。
  const stopFollow = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setFollowing(false);
  }, []);

  const openLogs = async (id: string) => {
    abortRef.current?.abort();
    setLog({ id, text: t('deployments.loading') });
    try {
      const res = await api.get<{ logs: string }>(`/api/admin/cm/instances/${id}/logs?tail=200`);
      setLog({ id, text: res.logs || '' });
    } catch (e) {
      setLog({ id, text: e instanceof Error ? e.message : t('common.loadFailed') });
    }
  };

  // 实时跟随：读取分块流并追加到日志框，滚动到底部。
  const followLogs = async (id: string) => {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setLog({ id, text: '' });
    setFollowing(true);
    try {
      const token = getToken();
      const resp = await fetch(`/api/admin/cm/instances/${id}/logs/stream?tail=200`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
        signal: ctrl.signal,
      });
      if (!resp.body) throw new Error(t('common.loadFailed'));
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        const text = decoder.decode(value, { stream: true });
        setLog((prev) => (prev ? { ...prev, text: prev.text + text } : prev));
        requestAnimationFrame(() => {
          const el = preRef.current;
          if (el) el.scrollTop = el.scrollHeight;
        });
      }
    } catch (e) {
      if (!(e instanceof DOMException && e.name === 'AbortError')) {
        setLog((prev) => (prev ? { ...prev, text: prev.text + `\n${e instanceof Error ? e.message : ''}` } : prev));
      }
    } finally {
      setFollowing(false);
    }
  };

  // 关闭日志框时中断跟随。
  const closeLogs = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setFollowing(false);
    setLog(null);
  };

  useEffect(() => {
    load();
    const timer = setInterval(load, 10000);
    return () => clearInterval(timer);
  }, [load]);

  const nodes: CMNodeStatus[] = useMemo(() => overview?.nodes ?? [], [overview]);
  const metricByName = useMemo(() => {
    const m = new Map<string, NodeMetric>();
    Object.values(metrics).forEach((v) => m.set(v.node_name, v));
    return m;
  }, [metrics]);

  // instance_id → Gateway 实例：补全容器的 服务/版本。
  const instanceById = useMemo(() => {
    const m = new Map<string, Instance>();
    instances.forEach((i) => m.set(i.id, i));
    return m;
  }, [instances]);

  // 稳定排序兜底：后端基于映射表产出，顺序不保证；按 节点 → 名称 → 实例 ID 固定展示序。
  const sortedContainers = useMemo(() => {
    return [...containers].sort((a, b) => {
      if (a.node_name !== b.node_name) return a.node_name < b.node_name ? -1 : 1;
      const an = a.name || a.container_id;
      const bn = b.name || b.container_id;
      if (an !== bn) return an < bn ? -1 : 1;
      return a.instance_id < b.instance_id ? -1 : 1;
    });
  }, [containers]);

  // 任务 / 事件按节点分组：以 gateway_id 关联，供节点详情内展示。
  // 分组后分别按时间倒序（最新在前）。
  const tasksByNode = useMemo(() => {
    const m = new Map<string, CMTask[]>();
    tasks.forEach((tk) => {
      if (!tk.node_id) return;
      const arr = m.get(tk.node_id);
      if (arr) arr.push(tk);
      else m.set(tk.node_id, [tk]);
    });
    m.forEach((arr) => arr.sort((a, b) => b.created_at_ms - a.created_at_ms));
    return m;
  }, [tasks]);

  const eventsByNode = useMemo(() => {
    const m = new Map<string, CMDockerEvent[]>();
    events.forEach((e) => {
      if (!e.node_id) return;
      const arr = m.get(e.node_id);
      if (arr) arr.push(e);
      else m.set(e.node_id, [e]);
    });
    m.forEach((arr) => arr.sort((a, b) => b.at - a.at));
    return m;
  }, [events]);

  // 未归属记录计数：节点未注册（gateway_id 为空）或节点已删除时，其任务/事件无宿主行。
  // 完全移入节点详情后这些记录不会展示，故给出计数提示，避免"数量凭空变少"的困惑。
  const nodeIdSet = useMemo(() => {
    const s = new Set<string>();
    nodes.forEach((n) => { if (n.gateway_id) s.add(n.gateway_id); });
    return s;
  }, [nodes]);
  const orphan = useMemo(() => {
    // 归属到已不存在的节点 id 的记录（node_id 非空但不在当前节点集内），累加记录数。
    let taskCount = 0;
    tasksByNode.forEach((arr, nodeId) => { if (!nodeIdSet.has(nodeId)) taskCount += arr.length; });
    let eventCount = 0;
    eventsByNode.forEach((arr, nodeId) => { if (!nodeIdSet.has(nodeId)) eventCount += arr.length; });
    // node_id 为空的记录不进入分组，同样无宿主行，计入未归属。
    taskCount += tasks.filter((tk) => !tk.node_id).length;
    eventCount += events.filter((e) => !e.node_id).length;
    return { tasks: taskCount, events: eventCount };
  }, [tasksByNode, eventsByNode, nodeIdSet, tasks, events]);

  const stats = overview?.stats;
  const lastReport = stats?.last_report_at ? new Date(stats.last_report_at).toLocaleString() : '-';

  if (!enabled) {
    return (
      <div>
        <PageHeader title={t('runtime.title')} />
        <p className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
          {t('runtime.notEnabled')}
        </p>
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title={t('runtime.title')}
        right={
          <div className="flex items-center gap-2 text-xs text-slate-400">
            <button onClick={load} className="rounded bg-slate-200 px-3 py-1.5 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">{t('common.refresh')}</button>
            {last && <span>{last}</span>}
          </div>
        }
      />
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {/* 观测概览 */}
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-3">
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-emerald-600">{stats?.node_up ?? 0}</p>
          <p className="text-xs text-slate-500">{t('runtime.nodeUp')}</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-sky-600">{stats?.containers ?? 0}</p>
          <p className="text-xs text-slate-500">{t('runtime.containers')}</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-sm font-semibold text-slate-600 dark:text-slate-300">{lastReport}</p>
          <p className="text-xs text-slate-500">{t('runtime.lastReport')}</p>
        </div>
      </div>

      {stats?.last_error && (
        <p className="mb-4 rounded bg-rose-50 px-3 py-2 text-xs text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
          {stats.last_error}
        </p>
      )}

      {/* 节点与 Agent 状态 */}
      <h2 className="mb-2 text-sm font-semibold text-slate-700 dark:text-slate-200">{t('runtime.nodes')}</h2>
      <div className="mb-6 overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('runtime.colNode')}</th>
              <th className="px-4 py-2">{t('runtime.colStatus')}</th>
              <th className="px-4 py-2">{t('runtime.colCpu')}</th>
              <th className="px-4 py-2">{t('runtime.colMemory')}</th>
              <th className="px-4 py-2">{t('runtime.colDisk')}</th>
              <th className="px-4 py-2">{t('runtime.lastSeen')}</th>
            </tr>
          </thead>
          <tbody>
            {nodes.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('runtime.noMetrics')}</td>
              </tr>
            )}
            {nodes.map((n) => {
              const mt = metricByName.get(n.name);
              const host = mt?.metrics?.host;
              const docker = mt?.metrics?.docker;
              const open = expanded.has(n.name);
              return (
                <Fragment key={n.name}>
                  <tr
                    onClick={() => toggleNode(n.name)}
                    className="cursor-pointer border-b border-slate-200 last:border-b-0 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40"
                  >
                    <td className="px-4 py-2">
                      <div className="flex items-center gap-2">
                        <span className="text-slate-400">{open ? '▾' : '▸'}</span>
                        <div>
                          <p className="font-semibold text-slate-700 dark:text-slate-200">{n.name}</p>
                          <p className="font-mono text-xs text-slate-400">{n.host}{n.region ? ` · ${n.region}` : ''}</p>
                        </div>
                      </div>
                    </td>
                    <td className="px-4 py-2">
                      <span className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${
                        n.healthy
                          ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300'
                          : 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300'
                      }`}>
                        {n.healthy ? t('runtime.agentOnline') : t('runtime.agentOffline')}
                      </span>
                    </td>
                    {host?.available ? (
                      <>
                        <td className="px-4 py-2"><div className="w-40"><UsageBar label="" pct={host.cpu_percent} /></div></td>
                        <td className="px-4 py-2"><div className="w-40"><UsageBar label="" pct={host.mem_percent} /></div></td>
                        <td className="px-4 py-2"><div className="w-40"><UsageBar label="" pct={host.disk_percent} /></div></td>
                      </>
                    ) : (
                      <td colSpan={3} className="px-4 py-2 text-xs text-slate-400">{mt?.error ?? t('runtime.metricsUnavailable')}</td>
                    )}
                    <td className="px-4 py-2 text-xs text-slate-400">{n.last_seen_ms ? new Date(n.last_seen_ms).toLocaleString() : '-'}</td>
                  </tr>
                  {open && (
                    <tr className="border-b border-slate-200 last:border-b-0 bg-slate-50/60 dark:border-slate-700 dark:bg-slate-900/30">
                      <td colSpan={6} className="px-4 py-3">
                        {host?.available ? (
                          <div className="flex flex-wrap gap-x-8 gap-y-2 text-xs text-slate-500 dark:text-slate-400">
                            <span>{t('runtime.memory')}: {fmtBytes(host.mem_used)} / {fmtBytes(host.mem_total)}</span>
                            <span>{t('runtime.disk')}: {fmtBytes(host.disk_used)} / {fmtBytes(host.disk_total)}</span>
                            <span>{t('runtime.dockerUsage')}: {fmtBytes(docker?.layers_size ?? 0)}</span>
                            {host.load_avg_1 !== undefined && <span>{t('runtime.load')}: {host.load_avg_1.toFixed(2)}</span>}
                            {!!host.uptime_sec && <span>{t('runtime.uptime')}: {fmtUptime(host.uptime_sec)}</span>}
                            <span>{t('runtime.dockerVersion')}: {n.docker_version || '-'}{n.docker_api_version ? ` (API ${n.docker_api_version})` : ''}</span>
                            <span>{t('runtime.gatewayId')}: {n.gateway_id || '-'}</span>
                          </div>
                        ) : (
                          <p className="text-xs text-slate-400">{mt?.error ?? t('runtime.metricsUnavailable')}</p>
                        )}

                        {!n.gateway_id ? (
                          <p className="mt-3 rounded bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
                            {t('runtime.nodeNotRegistered')}
                          </p>
                        ) : (
                          <>
                            {/* 最近任务 */}
                            <NodeTasks
                              tasks={tasksByNode.get(n.gateway_id) ?? []}
                              page={taskPages[n.name] ?? 1}
                              onPage={(p) => setTaskPages((prev) => ({ ...prev, [n.name]: p }))}
                              t={t}
                              onOpen={openTask}
                              onRetry={retryTask}
                              onCancel={cancelTask}
                            />
                            {/* 最近事件 */}
                            <NodeEvents
                              events={eventsByNode.get(n.gateway_id) ?? []}
                              expanded={moreEvents.has(n.name)}
                              onToggle={() => toggleSet(setMoreEvents, n.name)}
                              t={t}
                            />
                          </>
                        )}
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* 未归属记录提示：节点未注册或已删除时，其任务/事件不会出现在任何节点详情内。 */}
      {(orphan.tasks > 0 || orphan.events > 0) && (
        <p className="-mt-4 mb-6 text-xs text-slate-400 dark:text-slate-500">
          {t('runtime.orphanHint', { tasks: orphan.tasks, events: orphan.events })}
        </p>
      )}

      {/* 受管容器清单 */}
      <h2 className="mb-2 text-sm font-semibold text-slate-700 dark:text-slate-200">{t('runtime.containerList')}</h2>
      <div className="mb-6 overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('runtime.colInstance')}</th>
              <th className="px-4 py-2">{t('runtime.colName')}</th>
              <th className="px-4 py-2">{t('runtime.colImage')}</th>
              <th className="px-4 py-2">{t('runtime.colVersion')}</th>
              <th className="px-4 py-2">{t('runtime.colState')}</th>
              <th className="px-4 py-2">{t('runtime.colNode')}</th>
              <th className="px-4 py-2">{t('runtime.colPort')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {sortedContainers.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('runtime.noContainers')}</td>
              </tr>
            )}
            {sortedContainers.map((ct) => {
              const inst = instanceById.get(ct.instance_id);
              const version = inst?.version ?? '';
              const running = ct.state === 'running';
              return (
                <tr key={ct.container_id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                  <td className="px-4 py-2 font-mono text-xs">{ct.instance_id.slice(0, 8)}</td>
                  <td className="px-4 py-2">{ct.name || ct.container_id.slice(0, 12)}</td>
                  <td className="px-4 py-2 font-mono text-xs">{ct.image}</td>
                  <td className="px-4 py-2">{version || '-'}</td>
                  <td className="px-4 py-2"><StatusBadge value={ct.state} raw /></td>
                  <td className="px-4 py-2">{ct.node_name}</td>
                  <td className="px-4 py-2 font-mono text-xs">{ct.host_port || '-'}</td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap gap-1">
                      <ActionBtn onClick={() => openLogs(ct.instance_id)}>{t('deployments.logs')}</ActionBtn>
                      <ActionBtn onClick={() => act(ct.instance_id, 'restart')}>{t('deployments.restart')}</ActionBtn>
                      {running
                        ? <ActionBtn onClick={() => act(ct.instance_id, 'stop')}>{t('deployments.stop')}</ActionBtn>
                        : <ActionBtn onClick={() => act(ct.instance_id, 'start')}>{t('deployments.start')}</ActionBtn>}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* 运行时错误 */}
      <h2 className="mb-2 text-sm font-semibold text-slate-700 dark:text-slate-200">{t('runtime.errors')}</h2>
      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('runtime.colInstance')}</th>
              <th className="px-4 py-2">{t('runtime.colNode')}</th>
              <th className="px-4 py-2">{t('runtime.colState')}</th>
              <th className="px-4 py-2">{t('runtime.colExitCode')}</th>
              <th className="px-4 py-2">{t('runtime.colOom')}</th>
              <th className="px-4 py-2">{t('runtime.colTime')}</th>
            </tr>
          </thead>
          <tbody>
            {errors.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('runtime.noErrors')}</td>
              </tr>
            )}
            {errors.map((e) => (
              <tr key={`${e.node_name}-${e.instance_id}`} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-mono text-xs">{e.instance_id.slice(0, 8)}</td>
                <td className="px-4 py-2">{e.node_name}</td>
                <td className="px-4 py-2"><span className="rounded bg-rose-100 px-2 py-0.5 text-xs text-rose-700 dark:bg-rose-900/50 dark:text-rose-300">{e.state}</span></td>
                <td className="px-4 py-2 font-mono text-xs">{e.exit_code}</td>
                <td className="px-4 py-2 text-xs">{e.oom_killed ? t('runtime.oomYes') : '-'}</td>
                <td className="px-4 py-2 text-xs text-slate-400">{new Date(e.at * 1000).toLocaleString()}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Modal
        open={log !== null}
        title={`${t('deployments.logTitle')} · ${log?.id.slice(0, 8) ?? ''}`}
        onClose={closeLogs}
        maxWidth="max-w-4xl"
      >
        <div className="mb-2 flex items-center justify-end gap-2">
          {following ? (
            <button
              onClick={stopFollow}
              className="rounded bg-rose-500 px-3 py-1 text-xs text-white hover:bg-rose-600"
            >
              {t('deployments.stopFollow')}
            </button>
          ) : (
            <button
              onClick={() => log && followLogs(log.id)}
              className="rounded bg-emerald-500 px-3 py-1 text-xs text-white hover:bg-emerald-600"
            >
              {t('deployments.startFollow')}
            </button>
          )}
        </div>
        <pre
          ref={preRef}
          className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-4 font-mono text-xs leading-relaxed text-slate-100"
        >
          {log?.text || t('deployments.noInstances')}
        </pre>
      </Modal>

      <Modal
        open={taskDetail !== null}
        title={`${t('runtime.taskDetail')} · ${taskDetail?.id.slice(0, 12) ?? ''}`}
        onClose={() => setTaskDetail(null)}
        maxWidth="max-w-3xl"
      >
        {taskDetail && (
          <div className="space-y-3 text-sm">
            <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-xs">
              <div><span className="text-slate-400">{t('runtime.colAction')}: </span><span className="font-mono">{taskDetail.action}</span></div>
              <div><span className="text-slate-400">{t('runtime.colStatus')}: </span><StatusBadge value={taskDetail.status} raw /></div>
              <div><span className="text-slate-400">{t('runtime.colNode')}: </span><span className="font-mono">{taskDetail.node_id || '-'}</span></div>
              <div><span className="text-slate-400">{t('runtime.colProgress')}: </span>{taskDetail.percent ?? 0}%</div>
              <div><span className="text-slate-400">{t('runtime.createdBy')}: </span>{taskDetail.created_by || '-'}</div>
              <div><span className="text-slate-400">{t('runtime.attempts')}: </span>{taskDetail.attempts}</div>
              {taskDetail.parent_task_id && <div><span className="text-slate-400">{t('runtime.parentTask')}: </span><span className="font-mono">{taskDetail.parent_task_id.slice(0, 12)}</span></div>}
              <div><span className="text-slate-400">{t('runtime.colCreatedAt')}: </span>{taskDetail.created_at_ms ? new Date(taskDetail.created_at_ms).toLocaleString() : '-'}</div>
              {taskDetail.finished_at_ms ? <div><span className="text-slate-400">{t('runtime.finishedAt')}: </span>{new Date(taskDetail.finished_at_ms).toLocaleString()}</div> : null}
              {taskDetail.message && <div className="col-span-2"><span className="text-slate-400">{t('runtime.message')}: </span>{taskDetail.message}</div>}
              {taskDetail.error && <div className="col-span-2 text-rose-600 dark:text-rose-400"><span className="text-slate-400">{t('runtime.errorLabel')}: </span>{taskDetail.error}</div>}
            </div>
            <div>
              <p className="mb-1 text-xs font-semibold text-slate-500">{t('runtime.payload')}</p>
              <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-3 font-mono text-xs text-slate-100">
                {taskDetail.params ? JSON.stringify(taskDetail.params, null, 2) : '-'}
              </pre>
            </div>
            <div>
              <p className="mb-1 text-xs font-semibold text-slate-500">{t('runtime.result')}</p>
              <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-3 font-mono text-xs text-slate-100">
                {taskDetail.result ? JSON.stringify(taskDetail.result, null, 2) : '-'}
              </pre>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
