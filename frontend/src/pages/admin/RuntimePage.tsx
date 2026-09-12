import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api, ApiError } from '../../lib/client';
import type { CMOverview, CMNodeStatus, NodeMetric, CMRuntimeError } from '../../types';
import { PageHeader } from '../../themes';

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
    <div>
      <div className="mb-1 flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
        <span>{label}</span>
        <span className="font-mono">{pct.toFixed(1)}%</span>
      </div>
      <div className="h-1.5 w-full overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
        <div className={`h-full rounded-full ${usageColor(pct)}`} style={{ width: `${Math.min(pct, 100)}%` }} />
      </div>
    </div>
  );
}

export default function RuntimePage() {
  const { t } = useTranslation('admin');
  const [overview, setOverview] = useState<CMOverview | null>(null);
  const [metrics, setMetrics] = useState<Record<string, NodeMetric>>({});
  const [errors, setErrors] = useState<CMRuntimeError[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [last, setLast] = useState('');

  const load = useCallback(async () => {
    try {
      const [ov, mt, er] = await Promise.all([
        api.get<CMOverview>('/api/admin/cm/overview'),
        api.get<Record<string, NodeMetric>>('/api/admin/cm/metrics'),
        api.get<CMRuntimeError[]>('/api/admin/cm/errors'),
      ]);
      setOverview(ov);
      setMetrics(mt ?? {});
      setErrors(er ?? []);
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
      <div className="mb-6 grid gap-4 lg:grid-cols-2">
        {nodes.length === 0 && (
          <p className="rounded-xl border border-slate-200 bg-white p-6 text-center text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-500">{t('runtime.noMetrics')}</p>
        )}
        {nodes.map((n) => {
          const mt = metricByName.get(n.name);
          const host = mt?.metrics?.host;
          const docker = mt?.metrics?.docker;
          return (
            <div key={n.name} className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-700 dark:bg-slate-800">
              <div className="mb-3 flex items-center justify-between">
                <div>
                  <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">{n.name}</p>
                  <p className="font-mono text-xs text-slate-400">{n.host}{n.region ? ` · ${n.region}` : ''}</p>
                </div>
                <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${
                  n.healthy
                    ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300'
                    : 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300'
                }`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${n.healthy ? 'bg-emerald-500' : 'bg-rose-500'}`} />
                  {n.healthy ? t('runtime.agentOnline') : t('runtime.agentOffline')}
                </span>
              </div>

              {host?.available ? (
                <div className="grid gap-3 sm:grid-cols-3">
                  <UsageBar label={t('runtime.cpu')} pct={host.cpu_percent} />
                  <UsageBar label={t('runtime.memory')} pct={host.mem_percent} />
                  <UsageBar label={t('runtime.disk')} pct={host.disk_percent} />
                </div>
              ) : (
                <p className="text-xs text-slate-400">{mt?.error ?? t('runtime.metricsUnavailable')}</p>
              )}

              {host?.available && (
                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-slate-500 dark:text-slate-400">
                  <span>{t('runtime.memory')}: {fmtBytes(host.mem_used)} / {fmtBytes(host.mem_total)}</span>
                  <span>{t('runtime.disk')}: {fmtBytes(host.disk_used)} / {fmtBytes(host.disk_total)}</span>
                  <span>{t('runtime.dockerUsage')}: {fmtBytes(docker?.layers_size ?? 0)}</span>
                </div>
              )}
              <div className="mt-2 text-xs text-slate-400">
                {t('runtime.lastSeen')}: {n.last_seen_ms ? new Date(n.last_seen_ms).toLocaleString() : '-'}
              </div>
            </div>
          );
        })}
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
    </div>
  );
}
