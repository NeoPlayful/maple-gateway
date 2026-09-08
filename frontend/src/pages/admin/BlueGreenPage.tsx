import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import { create, remove } from '../../lib/modules';
import type { BlueGreenDeployment, Deployment, Service, Version } from '../../types';
import { ActionBtn } from '../../components/ui';

export default function BlueGreenPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<BlueGreenDeployment[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  const [open, setOpen] = useState(false);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [blueId, setBlueId] = useState('');
  const [greenId, setGreenId] = useState('');

  const load = useCallback(async () => {
    try {
      const d = await api.get<BlueGreenDeployment[]>('/api/admin/blue-green?limit=200');
      setRows(d);
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    (async () => {
      try {
        const s = await api.get<Service[]>('/api/admin/services?limit=200');
        setServices(s);
      } catch { /* ignore */ }
    })();
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setBlueId('');
    setGreenId('');
    try {
      const d = await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=100`);
      setDeploys(d);
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setBlueId('');
    setGreenId('');
    try {
      const v = await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`);
      setVersions(v);
    } catch { setVersions([]); }
  };

  const doCreate = async () => {
    setErr('');
    if (!depId || !blueId || !greenId || blueId === greenId) {
      setErr(t('bluegreen.errPickDeploy'));
      return;
    }
    try {
      await create('/api/admin/blue-green', {
        deployment_id: depId,
        blue_version_id: blueId,
        green_version_id: greenId,
      });
      setMsg(t('bluegreen.createSuccess'));
      setOpen(false);
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed'));
    }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    setMsg('');
    setErr('');
    try {
      await api.post(`/api/admin/blue-green/${id}/${act}`, body);
      setMsg(t('bluegreen.operateDone', { act }));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const depMeta = (id: string) => {
    const d = deploys.find((x) => x.id === id);
    const s = d ? services.find((x) => x.id === d.service_id) : undefined;
    return d ? `${s?.name ?? '?'} / ${d.name}` : id.slice(0, 8);
  };
  const verName = (id: string) => versions.find((v) => v.id === id)?.version ?? id.slice(0, 8);

  const activeOf = (r: BlueGreenDeployment) =>
    r.active_version_id ? verName(r.active_version_id) : verName(r.blue_version_id);
  const greenIdOf = (r: BlueGreenDeployment) =>
    r.green_version_id;
  const isBlueActive = (r: BlueGreenDeployment) =>
    r.active_version_id === r.blue_version_id || !r.active_version_id;

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('bluegreen.title')}</h1>
        <button onClick={() => setOpen((v) => !v)} className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover">
          {open ? t('common.collapse') : `+ ${t('bluegreen.newDeployment')}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.service')}</label>
              <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className={inputCls}>
                <option value="">{t('bluegreen.selectService')}</option>
                {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.deploymentName')}</label>
              <select value={depId} disabled={!svcId} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
                <option value="">{t('bluegreen.selectDeployment')}</option>
                {deploys.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
              </select>
            </div>
            <div />
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('bluegreen.blueVersion')}</label>
              <select value={blueId} disabled={!depId} onChange={(e) => setBlueId(e.target.value)} className={inputCls}>
                <option value="">{t('bluegreen.selectVersion')}</option>
                {versions.map((v) => <option key={v.id} value={v.id}>{v.version}</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('bluegreen.greenVersion')}</label>
              <select value={greenId} disabled={!depId} onChange={(e) => setGreenId(e.target.value)} className={inputCls}>
                <option value="">{t('bluegreen.selectVersion')}</option>
                {versions.map((v) => <option key={v.id} value={v.id}>{v.version}</option>)}
              </select>
            </div>
          </div>
          <button
            disabled={!blueId || !greenId || blueId === greenId}
            onClick={doCreate}
            className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-40 dark:bg-slate-700 dark:hover:bg-slate-600"
          >
            {t('common.create')}
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.deploymentName')}</th>
              <th className="px-4 py-2">Blue</th>
              <th className="px-4 py-2">Green</th>
              <th className="px-4 py-2">{t('bluegreen.currentActive')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr><td colSpan={5} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('bluegreen.none')}</td></tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 text-sm">{depMeta(r.deployment_id)}</td>
                <td className="px-4 py-2 text-xs">
                  {verName(r.blue_version_id)}
                  {isBlueActive(r) && <span className="ml-1 rounded bg-th-accent-soft-bg px-1.5 py-0.5 text-xs font-medium text-th-accent-soft-text">active</span>}
                </td>
                <td className="px-4 py-2 text-xs">
                  {verName(r.green_version_id)}
                  {!isBlueActive(r) && <span className="ml-1 rounded bg-th-accent-soft-bg px-1.5 py-0.5 text-xs font-medium text-th-accent-soft-text">active</span>}
                </td>
                <td className="px-4 py-2 text-xs font-mono">{activeOf(r)}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {isBlueActive(r) && greenIdOf(r) && (
                      <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: r.green_version_id })}>{t('bluegreen.switchToGreen')}</ActionBtn>
                    )}
                    {!isBlueActive(r) && (
                      <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: r.blue_version_id })}>{t('bluegreen.switchToBlue')}</ActionBtn>
                    )}
                    <ActionBtn onClick={() => action(r.id, 'rollback')}>{t('common.rollback')}</ActionBtn>
                    <ActionBtn danger onClick={async () => { if (!window.confirm(t('bluegreen.confirmDelete'))) return; await remove('/api/admin/blue-green', r.id); await load(); }}>{t('common.delete')}</ActionBtn>
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
