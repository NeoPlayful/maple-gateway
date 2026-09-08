import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import Sparkline, { type Pt } from '../../components/Sparkline';

interface Counts {
  tenants: number;
  domains: number;
  services: number;
  instances: number;
  nodes: number;
  deployments: number;
  versions: number;
  traffic_policies: number;
  rate_limits: number;
  canary: number;
  blue_green: number;
  admins: number;
}
interface Dist {
  total: number;
  routable?: number;
  by_status?: Record<string, number>;
}
interface CanaryRow {
  id: string;
  name: string;
  service_name?: string;
  phase: string;
  canary_weight: number;
  target_weight: number;
}
interface Overview {
  counts: Counts;
  instances: Dist & { by_health?: Record<string, number> };
  nodes: Dist;
  running_canary?: CanaryRow[];
  running_blue_green?: number;
  route_cache_hits: number;
  route_cache_misses: number;
}

// Dashboard 趋势序列（后端 dashboard/traffic|errors|latency）。
interface TrafficPoint {
  time: number;
  requests: number;
  errors: number;
  rate_limit_hits: number;
  rps: number;
  error_rate: number;
}
interface LatencyPoint {
  time: number;
  count: number;
  avg_ms: number;
  p95_ms: number;
}

function Card({ label, value, unit }: { label: string; value: number; unit?: string }) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
      <p className="text-sm text-slate-500 dark:text-slate-400">{label}</p>
      <p className="mt-1 text-3xl font-bold text-slate-800 dark:text-slate-100">
        {value}
        {unit && <span className="ml-1 text-base font-normal text-slate-400">{unit}</span>}
      </p>
    </div>
  );
}

export default function DashboardPage() {
  const { t } = useTranslation('admin');
  const [ov, setOv] = useState<Overview | null>(null);
  const [traffic, setTraffic] = useState<TrafficPoint[]>([]);
  const [latency, setLatency] = useState<LatencyPoint[]>([]);
  const [err, setErr] = useState('');

  const load = async () => {
    try {
      setOv(await api.get<Overview>('/api/admin/dashboard/overview'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('dashboard.errOverview'));
    }
    try {
      const [tt, l] = await Promise.all([
        api.get<TrafficPoint[]>('/api/admin/dashboard/traffic?minutes=30'),
        api.get<LatencyPoint[]>('/api/admin/dashboard/latency?minutes=30'),
      ]);
      setTraffic(tt);
      setLatency(l);
    } catch {
      /* 趋势不可用时不阻塞总览 */
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const reqPts: Pt[] = traffic.map((p) => ({ value: p.requests }));
  const errPts: Pt[] = traffic.map((p) => ({ value: p.errors }));
  const p95Pts: Pt[] = latency.map((p) => ({ value: Math.round(p.p95_ms) }));

  if (err) {
    return <div className="rounded-xl border border-slate-200 bg-white p-6 text-sm text-red-600 shadow-sm dark:border-slate-700 dark:bg-slate-800 dark:text-rose-400">{err}</div>;
  }
  if (!ov) {
    return <div className="text-sm text-slate-400 dark:text-slate-500">{t('app.loading')}</div>;
  }

  const c = ov.counts;
  const ih = ov.instances.by_health ?? {};
  const healthy = ih['healthy'] ?? 0;
  const unhealthy = ih['unhealthy'] ?? 0;
  const nodesOnline = ov.nodes.by_status?.['online'] ?? 0;
  const totalHits = ov.route_cache_hits + ov.route_cache_misses;

  const phaseText: Record<string, string> = {
    created: t('status.created'),
    running: t('status.running'),
    paused: t('status.paused'),
    completed: t('status.completed'),
    rolled_back: t('status.rolled_back'),
  };

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('dashboard.title')}</h1>
        <button onClick={load} className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">
          {t('common.refresh')}
        </button>
      </div>

      {/* 资源计数 */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
        <Card label={t('nav.tenants')} value={c.tenants} />
        <Card label={t('nav.domains')} value={c.domains} />
        <Card label={t('nav.services')} value={c.services} />
        <Card label={t('dashboard.deploymentsVersions')} value={c.deployments} unit={`/ ${c.versions}`} />
        <Card label={t('nav.instances')} value={c.instances} />
        <Card label={t('nav.nodes')} value={c.nodes} />
        <Card label={t('nav.traffic')} value={c.traffic_policies} />
        <Card label={t('nav.rateLimits')} value={c.rate_limits} />
        <Card label="Canary" value={c.canary} />
        <Card label="Blue/Green" value={c.blue_green} />
      </div>

      {/* 运行状态 */}
      <div className="mt-6 grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.instanceHealth')}</p>
          <div className="flex items-center gap-3">
            <div className="h-2.5 flex-1 overflow-hidden rounded-full bg-slate-200 dark:bg-slate-700">
              <div
                className="h-full rounded-full bg-emerald-500"
                style={{ width: `${ov.instances.total ? (healthy / ov.instances.total) * 100 : 0}%` }}
              />
            </div>
            <span className="text-sm font-semibold text-slate-700 dark:text-slate-200">{ov.instances.total}</span>
          </div>
          <div className="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
            <div className="rounded bg-emerald-50 py-2 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
              <div className="text-lg font-bold">{healthy}</div>{t('status.healthy')}
            </div>
            <div className="rounded bg-rose-50 py-2 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300">
              <div className="text-lg font-bold">{unhealthy}</div>{t('status.unhealthy')}
            </div>
            <div className="rounded bg-slate-100 py-2 text-slate-600 dark:bg-slate-700 dark:text-slate-300">
              <div className="text-lg font-bold">{ov.instances.routable ?? 0}</div>{t('dashboard.routable')}
            </div>
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.nodesTitle')}</p>
          <div className="grid grid-cols-2 gap-2 text-center text-xs">
            <div className="rounded bg-emerald-50 py-2 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
              <div className="text-lg font-bold">{nodesOnline}</div>online
            </div>
            <div className="rounded bg-slate-100 py-2 text-slate-600 dark:bg-slate-700 dark:text-slate-300">
              <div className="text-lg font-bold">{ov.nodes.total}</div>{t('dashboard.total')}
            </div>
          </div>
          <div className="mt-4 rounded bg-slate-50 p-3 text-xs text-slate-500 dark:bg-slate-900/50 dark:text-slate-400">
            {t('dashboard.cacheHit', { hits: ov.route_cache_hits })}
            {totalHits > 0 && <span>（{(ov.route_cache_hits / totalHits * 100).toFixed(1)}%）</span>}
            <span className="mx-1 text-slate-300">·</span>{t('dashboard.cacheMiss', { misses: ov.route_cache_misses })}
            <span className="mx-1 text-slate-300">·</span>BG {t('dashboard.running', { count: ov.running_blue_green ?? 0 })}
          </div>
        </div>

        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.runningCanary')}</p>
          {!ov.running_canary?.length ? (
            <p className="text-sm text-slate-400">{t('dashboard.noRunning')}</p>
          ) : (
            <ul className="space-y-2 text-sm">
              {ov.running_canary.map((r) => (
                <li key={r.id} className="flex items-center justify-between rounded bg-slate-50 px-3 py-2 dark:bg-slate-900/50">
                  <div>
                    <p className="font-medium text-slate-700 dark:text-slate-200">
                      {r.name}
                      {r.service_name && <span className="ml-1 text-xs text-slate-400">（{r.service_name}）</span>}
                    </p>
                  </div>
                  <div className="text-right text-xs text-slate-500">
                    <span className="mr-2 font-semibold text-slate-700 dark:text-slate-300">{phaseText[r.phase] ?? r.phase}</span>
                    {r.canary_weight}/{r.target_weight}%
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* 流量趋势（进程内时间桶，重启清零） */}
      <div className="mt-6 grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.trendRequests')}</p>
          <Sparkline data={reqPts} color="#0d9488" />
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.trendErrors')}</p>
          <Sparkline data={errPts} color="#e11d48" />
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="mb-3 text-sm font-medium text-slate-600 dark:text-slate-300">{t('dashboard.trendLatency')}</p>
          <Sparkline data={p95Pts} color="#6366f1" suffix=" ms" />
        </div>
      </div>
    </div>
  );
}
