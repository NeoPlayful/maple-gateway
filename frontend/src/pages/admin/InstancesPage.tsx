import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { list, create, statusAction } from '../../lib/modules';
import type { Service, Deployment, Instance as Inst, Version, Node } from '../../types';
import { StatusBadge, ActionBtn, Field } from '../../components/ui';

export default function InstancesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Inst[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  // 创建表单
  const [serviceId, setServiceId] = useState('');
  const [depId, setDepId] = useState('');
  const [verId, setVerId] = useState('');
  const [nodeId, setNodeId] = useState('');
  const [address, setAddress] = useState('');
  const [port, setPort] = useState('8080');
  const [weight, setWeight] = useState('1');

  const load = useCallback(async () => {
    try {
      setRows(await list('/api/admin/instances'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
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

  useEffect(() => {
    load();
    loadMeta();
  }, [load, loadMeta]);

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

  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? id.slice(0, 8);
  const nodeName = (id?: string | null) => nodes.find((n) => n.id === id)?.name ?? id?.slice(0, 8) ?? '-';

  const act = async (id: string, a: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction('/api/admin/instances', id, a);
      setMsg(t('common.operateSuccess'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const submit = async () => {
    setErr('');
    if (!serviceId || !address) {
      setErr(t('instances.errFill'));
      return;
    }
    try {
      await create('/api/admin/instances/register', {
        service_id: serviceId,
        ...(depId ? { deployment_id: depId } : {}),
        ...(verId ? { version_id: verId } : {}),
        ...(nodeId ? { node_id: nodeId } : {}),
        address,
        port: Number(port),
        weight: Number(weight) || 1,
      });
      setMsg(t('instances.registerSuccess'));
      setOpen(false);
      setAddress('');
      setServiceId('');
      setDepId('');
      setVerId('');
      setNodeId('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('instances.registerFailed'));
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('instances.title')}</h1>
        <button
          onClick={() => setOpen((v) => !v)}
          className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
        >
          {open ? t('common.collapse') : `+ ${t('instances.registerNew')}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <Field label={t('instances.parentService')}>
              <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className={inputCls}>
                <option value="">{t('canary.selectService')}</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </Field>
            <Field label={t('instances.deploymentOptional')}>
              <select value={depId} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
                <option value="">{t('instances.directService')}</option>
                {deploys.filter((d) => d.service_id === serviceId).map((d) => (
                  <option key={d.id} value={d.id}>{d.name}</option>
                ))}
              </select>
            </Field>
            <Field label={t('instances.versionOptional')}>
              <select value={verId} disabled={!depId} onChange={(e) => setVerId(e.target.value)} className={inputCls}>
                <option value="">{t('bluegreen.selectVersion')}</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version}</option>
                ))}
              </select>
            </Field>
            <Field label={t('instances.nodeOptional')}>
              <select value={nodeId} onChange={(e) => setNodeId(e.target.value)} className={inputCls}>
                <option value="">{t('instances.localhost')}</option>
                {nodes.map((n) => (
                  <option key={n.id} value={n.id}>{n.name}</option>
                ))}
              </select>
            </Field>
            <Field label={t('instances.addressIp')}>
              <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder={t('instances.addressPh')} className={inputCls} />
            </Field>
            <Field label={t('fields.port')}>
              <input type="number" value={port} onChange={(e) => setPort(e.target.value)} className={inputCls} />
            </Field>
            <Field label={t('fields.weight')}>
              <input type="number" value={weight} onChange={(e) => setWeight(e.target.value)} className={inputCls} />
            </Field>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:hover:bg-slate-600">
            {t('instances.register')}
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.service')}</th>
              <th className="px-4 py-2">{t('fields.address')}</th>
              <th className="px-4 py-2">{t('fields.node')}</th>
              <th className="px-4 py-2">{t('fields.health')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('instances.none')}</td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2">
                  {svcName(r.service_id)}
                  <span className="ml-1 text-xs text-slate-400">{r.version}</span>
                </td>
                <td className="px-4 py-2 font-mono text-xs">{r.address}:{r.port}</td>
                <td className="px-4 py-2">{nodeName(r.node_id)}</td>
                <td className="px-4 py-2"><StatusBadge value={r.health} /></td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'drain')}>{t('instances.drain')}</ActionBtn>}
                    {r.status === 'draining' && <ActionBtn onClick={() => act(r.id, 'undrain')}>{t('instances.undrain')}</ActionBtn>}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
