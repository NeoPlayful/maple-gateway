import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, ApiError } from '../../lib/client';
import { list } from '../../lib/modules';
import type { CMOverview, CMNodeStatus, NodeMetric, CMRuntimeError, CMContainer, Instance } from '../../types';
import { PageHeader } from '../../themes';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { Modal } from '../../components/admin/Modal';

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

export default function RuntimePage() {
  const { t } = useTranslation('admin');
  const [overview, setOverview] = useState<CMOverview | null>(null);
  const [metrics, setMetrics] = useState<Record<string, NodeMetric>>({});
  const [errors, setErrors] = useState<CMRuntimeError[]>([]);
  const [containers, setContainers] = useState<CMContainer[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [last, setLast] = useState('');
  const [log, setLog] = useState<{ id: string; text: string } | null>(null);
  // 展开的节点名集合：独立于轮询数据，10s 刷新不会收起已展开的详情。
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const toggleNode = (name: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });

  const load = useCallback(async () => {
    try {
      const [ov, mt, er, ct, ins] = await Promise.all([
        api.get<CMOverview>('/api/admin/cm/overview'),
        api.get<Record<string, NodeMetric>>('/api/admin/cm/metrics'),
        api.get<CMRuntimeError[]>('/api/admin/cm/errors'),
        api.get<CMContainer[]>('/api/admin/cm/containers'),
        list<Instance>('/api/admin/instances').catch(() => [] as Instance[]),
      ]);
      setOverview(ov);
      setMetrics(mt ?? {});
      setErrors(er ?? []);
      setContainers(ct ?? []);
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

  const openLogs = async (id: string) => {
    setLog({ id, text: t('deployments.loading') });
    try {
      const res = await api.get<{ logs: string }>(`/api/admin/cm/instances/${id}/logs?tail=200`);
      setLog({ id, text: res.logs || '' });
    } catch (e) {
      setLog({ id, text: e instanceof Error ? e.message : t('common.loadFailed') });
    }
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
                            <span>{t('runtime.gatewayId')}: {n.gateway_id || '-'}</span>
                          </div>
                        ) : (
                          <p className="text-xs text-slate-400">{mt?.error ?? t('runtime.metricsUnavailable')}</p>
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
        onClose={() => setLog(null)}
        maxWidth="max-w-4xl"
      >
        <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-4 font-mono text-xs leading-relaxed text-slate-100">
          {log?.text || t('deployments.noInstances')}
        </pre>
      </Modal>
    </div>
  );
}
