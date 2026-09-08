import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import type { CanaryRelease, Deployment, Service, Version } from '../../types';
import { PhaseBadge, ActionBtn } from '../../components/ui';

interface VersionMeta {
  id: string;
  version: string;
  deployment_id: string;
  service_id: string;
  weight: number;
  status: string;
}

export default function CanaryPage() {
  const { t } = useTranslation('admin');
  const [releases, setReleases] = useState<CanaryRelease[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  // 创建表单态。
  const [showCreate, setShowCreate] = useState(false);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [name, setName] = useState('');
  const [stableId, setStableId] = useState('');
  const [canaryId, setCanaryId] = useState('');
  const [initialWeight, setInitialWeight] = useState(10);

  const load = useCallback(async () => {
    try {
      const list = await api.get<CanaryRelease[]>('/api/admin/canary?limit=50');
      setReleases(list);
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    (async () => {
      try {
        const s = await api.get<Service[]>('/api/admin/services?limit=100');
        setServices(s);
      } catch { /* ignore */ }
    })();
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setStableId('');
    setCanaryId('');
    setVersions([]);
    try {
      const d = await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=50`);
      setDeploys(d);
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setStableId('');
    setCanaryId('');
    try {
      const v = await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`);
      const metas = v.map((x) => ({ ...x, deployment_id: deploymentId, service_id: svcId }));
      setVersions(metas);
    } catch { setVersions([]); }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    setMsg('');
    setErr('');
    try {
      await api.post(`/api/admin/canary/${id}/${act}`, body);
      setMsg(t('canary.operateDone', { act }));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const create = async () => {
    setErr('');
    try {
      await api.post('/api/admin/canary', {
        service_id: svcId,
        name,
        stable_version_id: stableId,
        canary_version_id: canaryId,
        initial_weight: initialWeight,
      });
      setMsg(t('canary.createHint'));
      setShowCreate(false);
      setName('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed'));
    }
  };

  const verName = (id: string) => versions.find((v) => v.id === id)?.version ?? id.slice(0, 8);
  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('canary.title')}</h1>
        <button
          onClick={() => setShowCreate((v) => !v)}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700 dark:bg-emerald-600 dark:hover:bg-emerald-700"
        >
          {showCreate ? t('common.collapse') : `+ ${t('canary.newRelease')}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {showCreate && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.service')}</label>
              <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className={inputCls}>
                <option value="">{t('canary.selectService')}</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.name')}</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('canary.namePh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.deploymentName')}</label>
              <select value={depId} disabled={!deploys.length} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
                <option value="">{t('canary.selectDeployment')}</option>
                {deploys.map((d) => (
                  <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('canary.initialWeight')}</label>
              <input type="number" min={1} max={100} value={initialWeight} onChange={(e) => setInitialWeight(Number(e.target.value))} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('canary.stableVersion')}</label>
              <select value={stableId} onChange={(e) => setStableId(e.target.value)} className={inputCls}>
                <option value="">{t('canary.selectStable')}</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('canary.canaryVersion')}</label>
              <select value={canaryId} onChange={(e) => setCanaryId(e.target.value)} className={inputCls}>
                <option value="">{t('canary.selectCanary')}</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                ))}
              </select>
            </div>
          </div>
          <button
            disabled={!stableId || !canaryId || stableId === canaryId}
            onClick={create}
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
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('fields.service')}</th>
              <th className="px-4 py-2">{t('fields.version')}</th>
              <th className="px-4 py-2">{t('fields.weight')}</th>
              <th className="px-4 py-2">{t('fields.phase')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {releases.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('canary.none')}</td></tr>
            )}
            {releases.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">{svcName(r.service_id)}</td>
                <td className="px-4 py-2 text-xs">
                  stable {verName(r.stable_version_id)} → canary {verName(r.canary_version_id)}
                </td>
                <td className="px-4 py-2">{r.canary_weight}% / {r.target_weight}%</td>
                <td className="px-4 py-2"><PhaseBadge phase={r.phase} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.phase === 'created' && (
                      <ActionBtn onClick={() => action(r.id, 'start')}>{t('canary.start')}</ActionBtn>
                    )}
                    {r.phase === 'running' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'pause')}>{t('canary.pause')}</ActionBtn>
                        <ActionBtn onClick={() => setWeightPrompt(r.id)}>{t('canary.adjustWeight')}</ActionBtn>
                        <ActionBtn onClick={() => action(r.id, 'promote')}>{t('canary.promote')}</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('canary.rollback')}</ActionBtn>
                      </>
                    )}
                    {r.phase === 'paused' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'resume')}>{t('canary.resume')}</ActionBtn>
                        <ActionBtn onClick={() => setWeightPrompt(r.id)}>{t('canary.adjustWeight')}</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('canary.rollback')}</ActionBtn>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );

  async function setWeightPrompt(releaseId: string) {
    const w = window.prompt(t('canary.weightPrompt'), '25');
    if (w === null) return;
    const n = Number(w);
    if (Number.isNaN(n) || n < 0 || n > 100) {
      setErr(t('canary.weightRange'));
      return;
    }
    await action(releaseId, 'weight', { weight: n });
  }
}
