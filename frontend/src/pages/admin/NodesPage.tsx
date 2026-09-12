import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { list, create, remove, statusAction } from '../../lib/modules';
import { api, ApiError } from '../../lib/client';
import type { Node, CMNodeStatus, NodeMetric } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { PageHeader } from '../../themes';

// 字节数格式化。
function fmtBytes(n?: number): string {
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

// 使用率配色。
function usageColor(pct: number): string {
  if (pct >= 90) return 'text-rose-600 dark:text-rose-400';
  if (pct >= 75) return 'text-amber-600 dark:text-amber-400';
  return 'text-emerald-600 dark:text-emerald-400';
}

function UsageCell({ pct }: { pct?: number }) {
  if (pct === undefined || pct === null) return <span className="text-slate-400">-</span>;
  return <span className={`font-mono text-xs ${usageColor(pct)}`}>{pct.toFixed(0)}%</span>;
}

export default function NodesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Node[]>([]);
  // CM 运行时视图：按节点名索引。
  const [cmNodes, setCmNodes] = useState<Record<string, CMNodeStatus>>({});
  const [cmMetrics, setCmMetrics] = useState<Record<string, NodeMetric>>({});
  const [cmEnabled, setCmEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [open, setOpen] = useState(false);
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 创建表单
  const [name, setName] = useState('');
  const [host, setHost] = useState('');
  const [region, setRegion] = useState('');

  const load = useCallback(async () => {
    try {
      setRows(await list<Node>('/api/admin/nodes'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  const loadRuntime = useCallback(async () => {
    try {
      const [ns, mt] = await Promise.all([
        api.get<CMNodeStatus[]>('/api/admin/cm/nodes'),
        api.get<Record<string, NodeMetric>>('/api/admin/cm/metrics'),
      ]);
      const byName: Record<string, CMNodeStatus> = {};
      (ns ?? []).forEach((n) => (byName[n.name] = n));
      setCmNodes(byName);
      setCmMetrics(mt ?? {});
      setCmEnabled(true);
    } catch (e) {
      if (e instanceof ApiError && e.status === 503) setCmEnabled(false);
    }
  }, []);

  useEffect(() => {
    load();
    loadRuntime();
    const timer = setInterval(loadRuntime, 10000);
    return () => clearInterval(timer);
  }, [load, loadRuntime]);

  const metricByName = useMemo(() => {
    const m = new Map<string, NodeMetric>();
    Object.values(cmMetrics).forEach((v) => m.set(v.node_name, v));
    return m;
  }, [cmMetrics]);

  const submit = async () => {
    setErr('');
    if (!name || !host) {
      setErr(t('nodes.errFill'));
      return;
    }
    try {
      await create('/api/admin/nodes', { name, host, region });
      toast.success(t('common.createSuccess'));
      setOpen(false);
      setName('');
      setHost('');
      setRegion('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed'));
    }
  };

  const act = async (id: string, a: string) => {
    try {
      await statusAction('/api/admin/nodes', id, a);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/nodes', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  const activeStates = ['active', 'enabled', 'online', 'healthy'];
  const inactiveStates = ['disabled', 'suspended', 'offline', 'maintenance', 'paused'];

  return (
    <div>
      <PageHeader
        title={t('nodes.title')}
        right={
          <button
            onClick={() => setOpen((v) => !v)}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {open ? t('common.collapse') : `+ ${t('common.new')}${t('nodes.title')}`}
          </button>
        }
      />
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}
      {!cmEnabled && (
        <p className="mb-3 rounded bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
          {t('runtime.notEnabled')}
        </p>
      )}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.nodeName')}</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('nodes.namePh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.host')}</label>
              <input value={host} onChange={(e) => setHost(e.target.value)} placeholder={t('nodes.hostPh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.region')}</label>
              <input value={region} onChange={(e) => setRegion(e.target.value)} className={inputCls} />
            </div>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:hover:bg-slate-600">
            {t('common.create')}
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.nodeName')}</th>
              <th className="px-4 py-2">{t('fields.host')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('nodes.agent')}</th>
              <th className="px-4 py-2">{t('runtime.colCpu')}</th>
              <th className="px-4 py-2">{t('runtime.colMemory')}</th>
              <th className="px-4 py-2">{t('runtime.colDisk')}</th>
              <th className="px-4 py-2">{t('runtime.colDocker')}</th>
              <th className="px-4 py-2">{t('runtime.lastSeen')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={10} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('common.none')}</td>
              </tr>
            )}
            {rows.map((r) => {
              const cn = cmNodes[r.name];
              const mt = metricByName.get(r.name);
              const hostm = mt?.metrics?.host;
              const docker = mt?.metrics?.docker;
              return (
                <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                  <td className="px-4 py-2">{r.name}</td>
                  <td className="px-4 py-2 font-mono text-xs">{r.host}</td>
                  <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                  <td className="px-4 py-2">
                    {cmEnabled && cn ? (
                      <span className={`inline-flex items-center gap-1.5 text-xs ${cn.healthy ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'}`}>
                        <span className={`h-1.5 w-1.5 rounded-full ${cn.healthy ? 'bg-emerald-500' : 'bg-rose-500'}`} />
                        {cn.healthy ? t('runtime.agentOnline') : t('runtime.agentOffline')}
                      </span>
                    ) : (
                      <span className="text-xs text-slate-400">-</span>
                    )}
                  </td>
                  <td className="px-4 py-2"><UsageCell pct={hostm?.available ? hostm.cpu_percent : undefined} /></td>
                  <td className="px-4 py-2">
                    {hostm?.available
                      ? <span className="font-mono text-xs">{fmtBytes(hostm.mem_used)} / {fmtBytes(hostm.mem_total)}</span>
                      : <span className="text-slate-400">-</span>}
                  </td>
                  <td className="px-4 py-2">
                    {hostm?.available
                      ? <span className="font-mono text-xs">{fmtBytes(hostm.disk_used)} / {fmtBytes(hostm.disk_total)}</span>
                      : <span className="text-slate-400">-</span>}
                  </td>
                  <td className="px-4 py-2">
                    {cn?.docker_version
                      ? <span className="font-mono text-xs">{cn.docker_version}<span className="ml-1 text-slate-400">{cn.cpus ?? ''}{(cn.cpus ?? 0) > 0 ? 'c' : ''}{docker?.containers !== undefined ? ` · ${docker.containers}` : ''}</span></span>
                      : <span className="text-slate-400">-</span>}
                  </td>
                  <td className="px-4 py-2 text-xs text-slate-400">
                    {cn?.last_seen_ms ? new Date(cn.last_seen_ms).toLocaleString() : (r.last_seen_at ? new Date(r.last_seen_at).toLocaleString() : '-')}
                  </td>
                  <td className="px-4 py-2">
                    <div className="flex flex-wrap gap-1">
                      {inactiveStates.includes(r.status)
                        ? <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>
                        : null}
                      {activeStates.includes(r.status)
                        ? <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>
                        : null}
                      <ActionBtn onClick={() => act(r.id, 'maintenance')}>{t('nodes.maintenance')}</ActionBtn>
                      <ActionBtn danger onClick={() => setDeleteId(r.id)}>{t('common.delete')}</ActionBtn>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle')}
        message={t('common.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
