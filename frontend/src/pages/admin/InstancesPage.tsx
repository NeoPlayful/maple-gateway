import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { list, create, update, remove, statusAction } from '../../lib/modules';
import { api, ApiError } from '../../lib/client';
import type { Service, Deployment, Instance as Inst, Version, Node, CMContainer } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

// 端口映射展示为「主机端口:容器内部端口」，如 8101:80；无映射则回退为 '-'。
function fmtPortPair(host: number, container: number): string {
  if (!host) return '-';
  return container ? `${host}:${container}` : String(host);
}

// 筛选栏：关键字（地址）+ 服务 + 健康 + 状态，纯前端过滤已加载列表。
const INSTANCE_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['address', 'version'], placeholder: 'instances.filterPh' },
  { type: 'select', key: 'service_id', placeholder: 'common.filterAllService', loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' } },
  { type: 'select', key: 'health', placeholder: 'common.filterAllHealth', labelPrefix: 'status' },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
];

export default function InstancesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Inst[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  // 运行时容器：instance_id → 容器状态（CM 观测）。
  const [containers, setContainers] = useState<Record<string, CMContainer>>({});
  const [cmEnabled, setCmEnabled] = useState(true);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 表单弹窗：editing 非空=编辑，空=注册。
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Inst | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  // 待删除实例 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 表单字段
  const [serviceId, setServiceId] = useState('');
  const [depId, setDepId] = useState('');
  const [verId, setVerId] = useState('');
  const [nodeId, setNodeId] = useState('');
  const [address, setAddress] = useState('');
  // 端口默认留空：不预设端口，由用户自行决定是否指定（留空则端点走 scheme 默认端口）。
  const [port, setPort] = useState('');
  const [weight, setWeight] = useState('1');
  const [protocol, setProtocol] = useState('http');

  const load = useCallback(async () => {
    try {
      setRows(await list('/api/admin/instances'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  const loadMeta = useCallback(async () => {
    try {
      const [s, d, n] = await Promise.all([
        list<Service>('/api/admin/services'),
        list<Deployment>('/api/admin/deployments'),
        list<Node>('/api/admin/nodes'),
      ]);
      setServices(s);
      setDeploys(d);
      setNodes(n);
    } catch {
      /* 忽略元数据加载失败 */
    }
  }, []);

  // 加载 CM 受管容器清单，建立 instance_id → 容器映射。
  const loadContainers = useCallback(async () => {
    try {
      const ct = await api.get<CMContainer[]>('/api/admin/cm/containers');
      const m: Record<string, CMContainer> = {};
      (ct ?? []).forEach((c) => (m[c.instance_id] = c));
      setContainers(m);
      setCmEnabled(true);
    } catch (e) {
      if (e instanceof ApiError && e.status === 503) setCmEnabled(false);
    }
  }, []);

  useEffect(() => {
    load();
    loadMeta();
    loadContainers();
  }, [load, loadMeta, loadContainers]);

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setVerId('');
    if (!deploymentId) {
      setVersions([]);
      return;
    }
    try {
      setVersions(await list<Version>(`/api/admin/deployments/${deploymentId}/versions`));
    } catch {
      setVersions([]);
    }
  };

  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? shortId(id);
  const nodeName = (id?: string | null) => nodes.find((n) => n.id === id)?.name ?? (id ? shortId(id) : '-');
  // 节点名优先取 CM 观测的容器节点名（与运行时页同源），回退到实例 node_id 匹配节点表。
  const nodeLabel = (r: Inst) => containers[r.id]?.node_name || (r.node_id ? nodeName(r.node_id) : '-');

  const fmtDep = (id?: string | null) => (id ? deploys.find((d) => d.id === id)?.name ?? shortId(id) : t('instances.directService'));
  const fmtVer = (id?: string | null) => (id ? versions.find((v) => v.id === id)?.version ?? shortId(id) : '-');

  const act = async (id: string, a: string) => {
    try {
      await statusAction('/api/admin/instances', id, a);
      toast.success(t('common.operateSuccess'));
      await load();
      await loadContainers();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/instances', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
      await loadContainers();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  // 打开注册弹窗：清空表单。
  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setServiceId('');
    setDepId('');
    setVerId('');
    setNodeId('');
    setVersions([]);
    setAddress('');
    setPort('');
    setWeight('1');
    setProtocol('http');
    setFormOpen(true);
  };

  // 打开编辑弹窗：回填可改字段，归属字段只读展示（service 不可改，归属需走 mount 接口，此处不改）。
  const openEdit = (r: Inst) => {
    setEditing(r);
    setFormErr('');
    setServiceId(r.service_id);
    setDepId(r.deployment_id || '');
    setVerId(r.version_id || '');
    setNodeId(r.node_id || '');
    setAddress(r.address);
    setPort(r.port ? String(r.port) : '');
    setWeight(String(r.weight));
    setProtocol(r.protocol || 'http');
    setVersions([]);
    if (r.deployment_id) {
      list<Version>(`/api/admin/deployments/${r.deployment_id}/versions`)
        .then(setVersions)
        .catch(() => setVersions([]));
    }
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  const submit = async () => {
    setFormErr('');
    if (!serviceId || !address) {
      setFormErr(t('instances.errFill'));
      return;
    }
    setBusy(true);
    try {
      if (editing) {
        // 编辑仅改运行参数；归属（deployment/version/node）经 mount 接口单独调整，此处不动。
        await update('/api/admin/instances', editing.id, {
          address,
          port: port ? Number(port) : 0,
          protocol,
          weight: Number(weight) || 1,
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await create('/api/admin/instances/register', {
          service_id: serviceId,
          ...(depId ? { deployment_id: depId } : {}),
          ...(verId ? { version_id: verId } : {}),
          ...(nodeId ? { node_id: nodeId } : {}),
          address,
          port: port ? Number(port) : 0,
          weight: Number(weight) || 1,
          protocol,
        });
        toast.success(t('instances.registerSuccess'));
      }
      closeForm();
      await load();
    } catch (e) {
      // SSRF 校验失败等后端错误直接透出原文，便于定位。
      setFormErr(
        e instanceof Error ? e.message : editing ? t('common.updateFailed') : t('instances.registerFailed'),
      );
    } finally {
      setBusy(false);
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  const filtered = useMemo(() => applyFilters(rows, INSTANCE_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  return (
    <div>
      <PageHeader
        title={t('instances.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('instances.registerNew')}`}
          </button>
        }
      />

      <Modal
        open={formOpen}
        title={editing ? t('instances.editTitle') : t('instances.registerTitle')}
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
              {busy ? t('common.saving') : editing ? t('common.save') : t('instances.register')}
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
          <Field label={t('instances.parentService')}>
            <select
              value={serviceId}
              disabled={!!editing}
              onChange={(e) => setServiceId(e.target.value)}
              className={inputCls}
            >
              <option value="">{t('releases.selectService')}</option>
              {services.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('instances.deploymentOptional')}>
            <select
              value={depId}
              disabled={!!editing}
              onChange={(e) => loadVersions(e.target.value)}
              className={inputCls}
            >
              <option value="">{t('instances.directService')}</option>
              {deploys.filter((d) => d.service_id === serviceId).map((d) => (
                <option key={d.id} value={d.id}>{d.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('instances.versionOptional')}>
            <select
              value={verId}
              disabled={!!editing || !depId}
              onChange={(e) => setVerId(e.target.value)}
              className={inputCls}
            >
              <option value="">{t('releases.selectVersion')}</option>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>{v.version}</option>
              ))}
            </select>
          </Field>
          <Field label={t('instances.nodeOptional')}>
            <select
              value={nodeId}
              disabled={!!editing}
              onChange={(e) => setNodeId(e.target.value)}
              className={inputCls}
            >
              <option value="">{t('instances.localhost')}</option>
              {nodes.map((n) => (
                <option key={n.id} value={n.id}>{n.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('instances.addressIp')}>
            <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder={t('instances.addressPh')} className={inputCls} />
          </Field>
          <Field label={t('fields.protocol')}>
            <select value={protocol} onChange={(e) => setProtocol(e.target.value)} className={inputCls}>
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </Field>
          <Field label={t('instances.portOptional')} hint={t('instances.portHint')}>
            <input type="number" min={0} max={65535} value={port} onChange={(e) => setPort(e.target.value)} placeholder={t('instances.portPh')} className={inputCls} />
          </Field>
          <Field label={t('fields.weight')}>
            <input type="number" value={weight} onChange={(e) => setWeight(e.target.value)} className={inputCls} />
          </Field>
        </div>
        {editing && (
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">
            {t('instances.ownershipHint')}
          </p>
        )}
      </Modal>

      <FilterBar
        filters={INSTANCE_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
        parents={{ '/api/admin/services': services }}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.service')}</th>
              <th className="px-4 py-2">{t('fields.address')}</th>
              <th className="px-4 py-2">{t('fields.node')}</th>
              <th className="px-4 py-2">{t('runtime.colIp')}</th>
              <th className="px-4 py-2">{t('runtime.colPort')}</th>
              <th className="px-4 py-2">{t('fields.health')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('instances.container')}</th>
              <th className="px-4 py-2">{t('instances.containerName')}</th>
              <th className="px-4 py-2">{t('instances.containerId')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={11} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{rows.length === 0 ? t('instances.none') : t('common.noMatch')}</td>
              </tr>
            )}
            {filtered.map((r) => {
              const ct = containers[r.id];
              return (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2">
                  {svcName(r.service_id)}
                  <span className="ml-1 text-xs text-slate-400">{r.version}</span>
                </td>
                <td className="px-4 py-2 font-mono text-xs">{r.port > 0 ? `${r.address}:${r.port}` : r.address}</td>
                <td className="px-4 py-2">{nodeLabel(r)}</td>
                <td className="px-4 py-2 font-mono text-xs">
                  {cmEnabled && ct?.ip ? ct.ip : <span className="text-xs text-slate-400">{cmEnabled ? '-' : ''}</span>}
                </td>
                <td className="px-4 py-2 font-mono text-xs">{ct ? fmtPortPair(ct.host_port, ct.container_port) : (r.port || '-')}</td>
                <td className="px-4 py-2"><StatusBadge value={r.health} /></td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  {cmEnabled && ct
                    ? <StatusBadge value={ct.state} raw label={ct.state === 'running' ? t('runtime.statusRunning') : undefined} />
                    : <span className="text-xs text-slate-400">{cmEnabled ? '-' : ''}</span>}
                </td>
                <td className="px-4 py-2 font-mono text-xs">
                  {cmEnabled && ct?.name
                    ? <span title={ct.name}>{ct.name}</span>
                    : <span className="text-xs text-slate-400">{cmEnabled ? '-' : ''}</span>}
                </td>
                <td className="px-4 py-2 font-mono text-xs">
                  {cmEnabled && ct?.container_id
                    ? <span title={ct.container_id}>{ct.container_id.slice(0, 12)}</span>
                    : <span className="text-xs text-slate-400">{cmEnabled ? '-' : ''}</span>}
                </td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'drain')}>{t('instances.drain')}</ActionBtn>}
                    {r.status === 'draining' && <ActionBtn onClick={() => act(r.id, 'undrain')}>{t('instances.undrain')}</ActionBtn>}
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
        message={t('instances.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
