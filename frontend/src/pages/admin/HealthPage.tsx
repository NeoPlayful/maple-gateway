import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import type { Instance, Service } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { PageHeader } from '../../themes';

export default function HealthPage() {
  const { t } = useTranslation('admin');
  const [instances, setInstances] = useState<Instance[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [err, setErr] = useState('');
  const [last, setLast] = useState('');

  const load = useCallback(async () => {
    try {
      const [i, s] = await Promise.all([
        api.get<Instance[]>('/api/admin/instances?limit=500'),
        api.get<Service[]>('/api/admin/services?limit=200'),
      ]);
      setInstances(i);
      setServices(s);
      setLast(new Date().toLocaleTimeString());
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, [load]);

  const svcName = useMemo(() => {
    const m = new Map<string, string>();
    services.forEach((s) => m.set(s.id, s.name));
    return (id: string) => m.get(id) ?? id.slice(0, 8);
  }, [services]);

  // 按服务分组（含未挂服务的空分组用不到，直接按实例的 service_id 归组）。
  const groups = useMemo(() => {
    const m = new Map<string, Instance[]>();
    for (const inst of instances) {
      const arr = m.get(inst.service_id) ?? [];
      arr.push(inst);
      m.set(inst.service_id, arr);
    }
    return [...m.entries()].sort((a, b) => svcName(a[0]).localeCompare(svcName(b[0])));
  }, [instances, svcName]);

  const counts = useMemo(() => {
    const c = { healthy: 0, unhealthy: 0, unknown: 0, recovering: 0 };
    for (const inst of instances) {
      if (inst.health in c) c[inst.health as keyof typeof c]++;
    }
    return c;
  }, [instances]);

  return (
    <div>
      <PageHeader
        title={t('health.title')}
        right={
          <div className="flex items-center gap-2 text-xs text-slate-400">
            <button onClick={load} className="rounded bg-slate-200 px-3 py-1.5 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">{t('common.refresh')}</button>
            {last && <span>{t('health.updatedAt', { time: last })}</span>}
          </div>
        }
      />
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {/* 健康汇总 */}
      <div className="mb-4 grid grid-cols-2 gap-3 md:grid-cols-4">
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-emerald-600">{counts.healthy}</p>
          <p className="text-xs text-slate-500">{t('health.healthySummary')}</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-rose-600">{counts.unhealthy}</p>
          <p className="text-xs text-slate-500">{t('health.unhealthySummary')}</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-amber-500">{counts.recovering}</p>
          <p className="text-xs text-slate-500">{t('health.recoveringSummary')}</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white p-4 text-center shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <p className="text-2xl font-bold text-slate-500">{counts.unknown}</p>
          <p className="text-xs text-slate-500">{t('health.unknownSummary')}</p>
        </div>
      </div>

      {groups.length === 0 && (
        <p className="rounded-xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-400 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-500">{t('instances.none')}</p>
      )}

      <div className="grid gap-4 lg:grid-cols-2">
        {groups.map(([serviceId, insts]) => (
          <div key={serviceId} className="rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-700 dark:bg-slate-800">
            <div className="mb-3 flex items-center justify-between">
              <p className="text-sm font-semibold text-slate-700 dark:text-slate-200">{svcName(serviceId)}</p>
              <p className="text-xs text-slate-400">{t('health.instanceCount', { count: insts.length })}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              {insts.map((inst) => (
                <div
                  key={inst.id}
                  className={`flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs ${
                    inst.health === 'healthy'
                      ? 'border-emerald-200 bg-emerald-50 dark:border-emerald-800 dark:bg-emerald-900/30'
                      : inst.health === 'unhealthy'
                        ? 'border-rose-200 bg-rose-50 dark:border-rose-800 dark:bg-rose-900/30'
                        : 'border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-700/40'
                  }`}
                >
                  <span className={`h-2 w-2 rounded-full ${
                    inst.health === 'healthy' ? 'bg-emerald-500'
                    : inst.health === 'unhealthy' ? 'bg-rose-500'
                    : 'bg-slate-400'
                  }`} />
                  <span className="font-mono text-slate-700 dark:text-slate-300">{inst.address}:{inst.port}</span>
                  <StatusBadge value={inst.health} />
                  <StatusBadge value={inst.status} />
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
