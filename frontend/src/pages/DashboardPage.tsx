import { useEffect, useState } from 'react';
import { api } from '../lib/client';

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

const phaseText: Record<string, string> = {
  created: '草稿',
  running: '进行中',
  paused: '已暂停',
  completed: '已完成',
  rolled_back: '已回滚',
};

function Card({ label, value, unit }: { label: string; value: number; unit?: string }) {
  return (
    <div className="rounded-xl bg-white p-5 shadow-sm">
      <p className="text-sm text-slate-500">{label}</p>
      <p className="mt-1 text-3xl font-bold text-slate-800">
        {value}
        {unit && <span className="ml-1 text-base font-normal text-slate-400">{unit}</span>}
      </p>
    </div>
  );
}

export default function DashboardPage() {
  const [ov, setOv] = useState<Overview | null>(null);
  const [err, setErr] = useState('');

  const load = async () => {
    try {
      setOv(await api.get<Overview>('/api/admin/dashboard/overview'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '总览加载失败');
    }
  };

  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, []);

  if (err) {
    return <div className="rounded-xl bg-white p-6 text-sm text-red-600 shadow-sm">{err}</div>;
  }
  if (!ov) {
    return <div className="text-sm text-slate-400">加载中…</div>;
  }

  const c = ov.counts;
  const ih = ov.instances.by_health ?? {};
  const healthy = ih['healthy'] ?? 0;
  const unhealthy = ih['unhealthy'] ?? 0;
  const nodesOnline = ov.nodes.by_status?.['online'] ?? 0;
  const totalHits = ov.route_cache_hits + ov.route_cache_misses;

  return (
    <div>
      <div className="mb-6 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">Dashboard</h1>
        <button onClick={load} className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-300">
          刷新
        </button>
      </div>

      {/* 资源计数 */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
        <Card label="租户" value={c.tenants} />
        <Card label="域名" value={c.domains} />
        <Card label="服务" value={c.services} />
        <Card label="部署/版本" value={c.deployments} unit={`/ ${c.versions}`} />
        <Card label="实例" value={c.instances} />
        <Card label="节点" value={c.nodes} />
        <Card label="流量策略" value={c.traffic_policies} />
        <Card label="限流规则" value={c.rate_limits} />
        <Card label="Canary" value={c.canary} />
        <Card label="Blue/Green" value={c.blue_green} />
      </div>

      {/* 运行状态 */}
      <div className="mt-6 grid gap-4 lg:grid-cols-3">
        <div className="rounded-xl bg-white p-5 shadow-sm">
          <p className="mb-3 text-sm font-medium text-slate-600">实例健康</p>
          <div className="flex items-center gap-3">
            <div className="h-2.5 flex-1 overflow-hidden rounded-full bg-slate-200">
              <div
                className="h-full rounded-full bg-emerald-500"
                style={{ width: `${ov.instances.total ? (healthy / ov.instances.total) * 100 : 0}%` }}
              />
            </div>
            <span className="text-sm font-semibold text-slate-700">{ov.instances.total}</span>
          </div>
          <div className="mt-3 grid grid-cols-3 gap-2 text-center text-xs">
            <div className="rounded bg-emerald-50 py-2 text-emerald-700">
              <div className="text-lg font-bold">{healthy}</div>健康
            </div>
            <div className="rounded bg-rose-50 py-2 text-rose-700">
              <div className="text-lg font-bold">{unhealthy}</div>异常
            </div>
            <div className="rounded bg-slate-100 py-2 text-slate-600">
              <div className="text-lg font-bold">{ov.instances.routable ?? 0}</div>可路由
            </div>
          </div>
        </div>

        <div className="rounded-xl bg-white p-5 shadow-sm">
          <p className="mb-3 text-sm font-medium text-slate-600">节点</p>
          <div className="grid grid-cols-2 gap-2 text-center text-xs">
            <div className="rounded bg-emerald-50 py-2 text-emerald-700">
              <div className="text-lg font-bold">{nodesOnline}</div>online
            </div>
            <div className="rounded bg-slate-100 py-2 text-slate-600">
              <div className="text-lg font-bold">{ov.nodes.total}</div>总计
            </div>
          </div>
          <div className="mt-4 rounded bg-slate-50 p-3 text-xs text-slate-500">
            路由缓存命中 {ov.route_cache_hits}
            {totalHits > 0 && <span>（{(ov.route_cache_hits / totalHits * 100).toFixed(1)}%）</span>}
            <span className="mx-1 text-slate-300">·</span>未命中 {ov.route_cache_misses}
            <span className="mx-1 text-slate-300">·</span>BG {ov.running_blue_green ?? 0} 进行中
          </div>
        </div>

        <div className="rounded-xl bg-white p-5 shadow-sm">
          <p className="mb-3 text-sm font-medium text-slate-600">进行中的 Canary</p>
          {!ov.running_canary?.length ? (
            <p className="text-sm text-slate-400">暂无进行中的发布</p>
          ) : (
            <ul className="space-y-2 text-sm">
              {ov.running_canary.map((r) => (
                <li key={r.id} className="flex items-center justify-between rounded bg-slate-50 px-3 py-2">
                  <div>
                    <p className="font-medium text-slate-700">
                      {r.name}
                      {r.service_name && <span className="ml-1 text-xs text-slate-400">（{r.service_name}）</span>}
                    </p>
                  </div>
                  <div className="text-right text-xs text-slate-500">
                    <span className="mr-2 font-semibold text-slate-700">{phaseText[r.phase] ?? r.phase}</span>
                    {r.canary_weight}/{r.target_weight}%
                  </div>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </div>
  );
}
