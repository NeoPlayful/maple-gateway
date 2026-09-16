import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { list, create, update, remove, statusAction } from '../../lib/modules';
import { api, ApiError } from '../../lib/client';
import type { Node, CMNodeStatus, NodeMetric } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';

// 筛选栏：关键字（节点名/主机地址）+ 状态 + 区域，纯前端过滤已加载列表。
const NODE_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name', 'host'], placeholder: 'nodes.filterPh' },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
  { type: 'select', key: 'region', placeholder: 'common.filterAllRegion' },
];

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
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 表单弹窗：editing 非空=编辑，空=新建。
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Node | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');

  // 表单字段
  const [name, setName] = useState('');
  const [host, setHost] = useState('');
  const [region, setRegion] = useState('');
  const [weight, setWeight] = useState('0');

  const load = useCallback(async () => {
    try {
      setRows(await list<Node>('/api/admin/nodes'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
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

  // 打开新建弹窗：清空表单。
  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setName('');
    setHost('');
    setRegion('');
    setWeight('0');
    setFormOpen(true);
  };

  // 打开编辑弹窗：回填可改字段；name 只读（后端 Update 不支持改名称）。
  const openEdit = (r: Node) => {
    setEditing(r);
    setFormErr('');
    setName(r.name);
    setHost(r.host);
    setRegion(r.region ?? '');
    setWeight(String(r.weight ?? 0));
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  const submit = async () => {
    setFormErr('');
    if (!editing && !name) {
      setFormErr(t('nodes.errFill'));
      return;
    }
    if (!host.trim()) {
      setFormErr(t('nodes.errFill'));
      return;
    }
    setBusy(true);
    try {
      if (editing) {
        await update('/api/admin/nodes', editing.id, {
          host: host.trim(),
          region,
          weight: Number(weight) || 0,
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await create('/api/admin/nodes', {
          name,
          host: host.trim(),
          region,
          weight: Number(weight) || 0,
        });
        toast.success(t('common.createSuccess'));
      }
      closeForm();
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : editing ? t('common.updateFailed') : t('common.createFailed'));
    } finally {
      setBusy(false);
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
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  const activeStates = ['active', 'enabled', 'online', 'healthy'];
  const inactiveStates = ['disabled', 'suspended', 'offline', 'maintenance', 'paused'];

  const filtered = useMemo(() => applyFilters(rows, NODE_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  return (
    <div>
      <PageHeader
        title={t('nodes.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('nodes.newTitle')}`}
          </button>
        }
      />
      {!cmEnabled && (
        <p className="mb-3 rounded bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
          {t('runtime.notEnabled')}
        </p>
      )}

      <Modal
        open={formOpen}
        title={editing ? t('nodes.editTitle') : t('nodes.newTitle')}
        onClose={closeForm}
        footer={
          <>
            <button
              onClick={closeForm}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={submit}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : editing ? t('common.save') : t('common.create')}
            </button>
          </>
        }
      >
        {formErr && (
          <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
            {formErr}
          </p>
        )}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('fields.nodeName')}>
            <input
              value={name}
              disabled={!!editing}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('nodes.namePh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('fields.host')}>
            <input value={host} onChange={(e) => setHost(e.target.value)} placeholder={t('nodes.hostPh')} className={inputCls} />
          </Field>
          <Field label={t('fields.region')}>
            <input value={region} onChange={(e) => setRegion(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('fields.weight')}>
            <input type="number" value={weight} onChange={(e) => setWeight(e.target.value)} className={inputCls} />
          </Field>
        </div>
        {editing && (
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('nodes.ownershipHint')}</p>
        )}
      </Modal>

      <FilterBar
        filters={NODE_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
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
            {filtered.length === 0 && (
              <tr>
                <td colSpan={10} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{rows.length === 0 ? t('common.none') : t('common.noMatch')}</td>
              </tr>
            )}
            {filtered.map((r) => {
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
                      <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
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
