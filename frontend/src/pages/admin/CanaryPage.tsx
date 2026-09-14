import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import { list, create, update, remove } from '../../lib/modules';
import type { CanaryRelease, Deployment, Service, Version } from '../../types';
import { PhaseBadge } from '../../components/admin/PhaseBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { PageHeader } from '../../themes';

interface VersionMeta {
  id: string;
  version: string;
  deployment_id: string;
  service_id: string;
  weight: number;
  status: string;
}

// 可删除的终态（后端 running/paused 拒绝删除）。
const TERMINAL_PHASES = ['created', 'completed', 'rolled_back', 'failed'];

export default function CanaryPage() {
  const { t } = useTranslation('admin');
  const [releases, setReleases] = useState<CanaryRelease[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<VersionMeta[]>([]);

  // 新建弹窗。
  const [createOpen, setCreateOpen] = useState(false);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [name, setName] = useState('');
  const [stableId, setStableId] = useState('');
  const [canaryId, setCanaryId] = useState('');
  const [initialWeight, setInitialWeight] = useState(10);

  // 编辑弹窗：editing 非空=编辑（只改 name/target_weight/step_weight）。
  const [editing, setEditing] = useState<CanaryRelease | null>(null);
  const [editName, setEditName] = useState('');
  const [editTarget, setEditTarget] = useState('100');
  const [editStep, setEditStep] = useState('10');

  // 调权重弹窗：weightTarget 非空=打开。
  const [weightTarget, setWeightTarget] = useState<CanaryRelease | null>(null);
  const [weightVal, setWeightVal] = useState('25');

  // 待删除发布 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');

  const load = useCallback(async () => {
    try {
      setReleases(await list<CanaryRelease>('/api/admin/canary'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    list<Service>('/api/admin/services').then(setServices).catch(() => {});
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setStableId('');
    setCanaryId('');
    setVersions([]);
    if (!serviceId) {
      setDeploys([]);
      return;
    }
    try {
      setDeploys(await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=50`));
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setStableId('');
    setCanaryId('');
    try {
      const v = await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`);
      setVersions(v.map((x) => ({ ...x, deployment_id: deploymentId, service_id: svcId })));
    } catch { setVersions([]); }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    try {
      await api.post(`/api/admin/canary/${id}/${act}`, body);
      toast.success(t('canary.operateDone', { act }));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doCreate = async () => {
    setFormErr('');
    if (!svcId || !name || !stableId || !canaryId) {
      setFormErr(t('canary.errFill'));
      return;
    }
    if (stableId === canaryId) {
      setFormErr(t('canary.sameVersion'));
      return;
    }
    setBusy(true);
    try {
      await create('/api/admin/canary', {
        service_id: svcId,
        name,
        stable_version_id: stableId,
        canary_version_id: canaryId,
        initial_weight: initialWeight,
      });
      toast.success(t('canary.createHint'));
      setCreateOpen(false);
      setName('');
      setSvcId('');
      setDepId('');
      setStableId('');
      setCanaryId('');
      setVersions([]);
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  // 打开编辑弹窗：回填可改字段（name/target_weight/step_weight）。
  const openEdit = (r: CanaryRelease) => {
    setEditing(r);
    setFormErr('');
    setEditName(r.name);
    setEditTarget(String(r.target_weight ?? 100));
    setEditStep(String(r.step_weight ?? 10));
  };

  const closeEdit = () => {
    setEditing(null);
  };

  const doEdit = async () => {
    if (!editing) return;
    setFormErr('');
    if (!editName) {
      setFormErr(t('canary.errFill'));
      return;
    }
    setBusy(true);
    try {
      await update('/api/admin/canary', editing.id, {
        name: editName,
        target_weight: Number(editTarget) || 100,
        step_weight: Number(editStep) || 10,
      });
      toast.success(t('common.updateSuccess'));
      setEditing(null);
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.updateFailed'));
    } finally {
      setBusy(false);
    }
  };

  const openWeight = (r: CanaryRelease) => {
    setWeightTarget(r);
    setWeightVal(String(r.canary_weight ?? 25));
  };

  const doWeight = async () => {
    if (!weightTarget) return;
    const n = Number(weightVal);
    if (Number.isNaN(n) || n < 0 || n > 100) {
      toast.error(t('canary.weightRange'));
      return;
    }
    setBusy(true);
    try {
      await api.post(`/api/admin/canary/${weightTarget.id}/weight`, { weight: n });
      toast.success(t('canary.operateDone', { act: 'weight' }));
      setWeightTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/canary', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const verName = (id: string) => versions.find((v) => v.id === id)?.version ?? id.slice(0, 8);
  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <PageHeader
        title={t('canary.title')}
        right={
          <button
            onClick={() => {
              setFormErr('');
              setCreateOpen(true);
            }}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('canary.newRelease')}`}
          </button>
        }
      />

      {/* 新建弹窗 */}
      <Modal
        open={createOpen}
        title={t('canary.newRelease')}
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
              <option value="">{t('canary.selectService')}</option>
              {services.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('fields.name')}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('canary.namePh')} className={inputCls} />
          </Field>
          <Field label={t('fields.deploymentName')}>
            <select value={depId} disabled={!deploys.length} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
              <option value="">{t('canary.selectDeployment')}</option>
              {deploys.map((d) => (
                <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>
              ))}
            </select>
          </Field>
          <Field label={t('canary.initialWeight')}>
            <input type="number" min={1} max={100} value={initialWeight} onChange={(e) => setInitialWeight(Number(e.target.value))} className={inputCls} />
          </Field>
          <Field label={t('canary.stableVersion')}>
            <select value={stableId} onChange={(e) => setStableId(e.target.value)} className={inputCls}>
              <option value="">{t('canary.selectStable')}</option>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
              ))}
            </select>
          </Field>
          <Field label={t('canary.canaryVersion')}>
            <select value={canaryId} onChange={(e) => setCanaryId(e.target.value)} className={inputCls}>
              <option value="">{t('canary.selectCanary')}</option>
              {versions.map((v) => (
                <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
              ))}
            </select>
          </Field>
        </div>
      </Modal>

      {/* 编辑弹窗 */}
      <Modal
        open={editing !== null}
        title={t('canary.editTitle')}
        onClose={closeEdit}
        footer={
          <>
            <button
              onClick={closeEdit}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={doEdit}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : t('common.save')}
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
            <input value={editing ? svcName(editing.service_id) : ''} readOnly disabled className={inputCls} />
          </Field>
          <Field label={t('fields.name')}>
            <input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder={t('canary.namePh')} className={inputCls} />
          </Field>
          <Field label={t('canary.stableVersion')}>
            <input value={editing ? verName(editing.stable_version_id) : ''} readOnly disabled className={inputCls} />
          </Field>
          <Field label={t('canary.canaryVersion')}>
            <input value={editing ? verName(editing.canary_version_id) : ''} readOnly disabled className={inputCls} />
          </Field>
          <Field label={t('canary.targetWeight')}>
            <input type="number" min={1} max={100} value={editTarget} onChange={(e) => setEditTarget(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('canary.stepWeight')}>
            <input type="number" min={1} max={100} value={editStep} onChange={(e) => setEditStep(e.target.value)} className={inputCls} />
          </Field>
        </div>
        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('canary.ownershipHint')}</p>
      </Modal>

      {/* 调权重弹窗 */}
      <Modal
        open={weightTarget !== null}
        title={t('canary.setWeightTitle')}
        onClose={() => setWeightTarget(null)}
        maxWidth="max-w-sm"
        footer={
          <>
            <button
              onClick={() => setWeightTarget(null)}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={doWeight}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : t('common.save')}
            </button>
          </>
        }
      >
        <Field label={t('canary.weightLabel')}>
          <input type="number" min={0} max={100} value={weightVal} onChange={(e) => setWeightVal(e.target.value)} className={inputCls} />
        </Field>
      </Modal>

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
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
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    {r.phase === 'created' && (
                      <ActionBtn onClick={() => action(r.id, 'start')}>{t('canary.start')}</ActionBtn>
                    )}
                    {r.phase === 'running' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'pause')}>{t('canary.pause')}</ActionBtn>
                        <ActionBtn onClick={() => openWeight(r)}>{t('canary.adjustWeight')}</ActionBtn>
                        <ActionBtn onClick={() => action(r.id, 'promote')}>{t('canary.promote')}</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('canary.rollback')}</ActionBtn>
                      </>
                    )}
                    {r.phase === 'paused' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'resume')}>{t('canary.resume')}</ActionBtn>
                        <ActionBtn onClick={() => openWeight(r)}>{t('canary.adjustWeight')}</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('canary.rollback')}</ActionBtn>
                      </>
                    )}
                    {TERMINAL_PHASES.includes(r.phase) && (
                      <ActionBtn danger onClick={() => setDeleteId(r.id)}>{t('common.delete')}</ActionBtn>
                    )}
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
        message={t('canary.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
