import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import { create, remove } from '../../lib/modules';
import type { BlueGreenDeployment, Deployment, Service, Version } from '../../types';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { PageHeader } from '../../themes';

export default function BlueGreenPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<BlueGreenDeployment[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);

  const [createOpen, setCreateOpen] = useState(false);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [blueId, setBlueId] = useState('');
  const [greenId, setGreenId] = useState('');

  // 待删除蓝绿部署 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');

  const load = useCallback(async () => {
    try {
      setRows(await api.get<BlueGreenDeployment[]>('/api/admin/blue-green?limit=200'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    api.get<Service[]>('/api/admin/services?limit=200').then(setServices).catch(() => {});
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setBlueId('');
    setGreenId('');
    if (!serviceId) {
      setDeploys([]);
      return;
    }
    try {
      setDeploys(await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=100`));
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setBlueId('');
    setGreenId('');
    try {
      setVersions(await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`));
    } catch { setVersions([]); }
  };

  const doCreate = async () => {
    setFormErr('');
    if (!depId || !blueId || !greenId) {
      setFormErr(t('bluegreen.errPickDeploy'));
      return;
    }
    if (blueId === greenId) {
      setFormErr(t('bluegreen.sameVersion'));
      return;
    }
    setBusy(true);
    try {
      await create('/api/admin/blue-green', {
        deployment_id: depId,
        blue_version_id: blueId,
        green_version_id: greenId,
      });
      toast.success(t('bluegreen.createSuccess'));
      setCreateOpen(false);
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    try {
      await api.post(`/api/admin/blue-green/${id}/${act}`, body);
      toast.success(t('bluegreen.operateDone', { act }));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/blue-green', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
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
  const isBlueActive = (r: BlueGreenDeployment) =>
    r.active_version_id === r.blue_version_id || !r.active_version_id;

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <PageHeader
        title={t('bluegreen.title')}
        right={
          <button
            onClick={() => {
              setFormErr('');
              setCreateOpen(true);
            }}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('bluegreen.newDeployment')}`}
          </button>
        }
      />

      <Modal
        open={createOpen}
        title={t('bluegreen.newDeployment')}
        onClose={() => setCreateOpen(false)}
        footer={
          <>
            <button
              onClick={() => setCreateOpen(false)}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={doCreate}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : t('common.create')}
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
          <Field label={t('fields.service')}>
            <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className={inputCls}>
              <option value="">{t('bluegreen.selectService')}</option>
              {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          </Field>
          <Field label={t('fields.deploymentName')}>
            <select value={depId} disabled={!svcId} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
              <option value="">{t('bluegreen.selectDeployment')}</option>
              {deploys.map((d) => <option key={d.id} value={d.id}>{d.name}</option>)}
            </select>
          </Field>
          <Field label={t('bluegreen.blueVersion')}>
            <select value={blueId} disabled={!depId} onChange={(e) => setBlueId(e.target.value)} className={inputCls}>
              <option value="">{t('bluegreen.selectVersion')}</option>
              {versions.map((v) => <option key={v.id} value={v.id}>{v.version}</option>)}
            </select>
          </Field>
          <Field label={t('bluegreen.greenVersion')}>
            <select value={greenId} disabled={!depId} onChange={(e) => setGreenId(e.target.value)} className={inputCls}>
              <option value="">{t('bluegreen.selectVersion')}</option>
              {versions.map((v) => <option key={v.id} value={v.id}>{v.version}</option>)}
            </select>
          </Field>
        </div>
      </Modal>

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
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
                    {isBlueActive(r) && r.green_version_id && (
                      <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: r.green_version_id })}>{t('bluegreen.switchToGreen')}</ActionBtn>
                    )}
                    {!isBlueActive(r) && (
                      <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: r.blue_version_id })}>{t('bluegreen.switchToBlue')}</ActionBtn>
                    )}
                    <ActionBtn onClick={() => action(r.id, 'rollback')}>{t('common.rollback')}</ActionBtn>
                    <ActionBtn danger onClick={() => setDeleteId(r.id)}>{t('common.delete')}</ActionBtn>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle')}
        message={t('bluegreen.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
