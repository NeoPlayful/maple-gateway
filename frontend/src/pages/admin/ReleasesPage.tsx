import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import { create, update, remove } from '../../lib/modules';
import type { Release, ReleaseEvent, Deployment, Service, Version } from '../../types';
import { PhaseBadge } from '../../components/admin/PhaseBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

type Strategy = 'canary' | 'bluegreen';

// 筛选栏：关键字（发布名）+ 服务 + 阶段，纯前端过滤已加载列表（策略按钮组另走服务端过滤）。
const RELEASE_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name'], placeholder: 'releases.filterPh' },
  { type: 'select', key: 'service_id', placeholder: 'common.filterAllService', loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' } },
  { type: 'select', key: 'phase', placeholder: 'common.filterAllPhase' },
];

interface VersionMeta {
  id: string;
  version: string;
  deployment_id: string;
  service_id: string;
  weight: number;
  status: string;
}

// 可删除的终态（后端活跃中拒绝删除）。
const TERMINAL_PHASES = ['completed', 'rolled_back', 'failed'];

// canary 的 config 形态。
interface CanaryConfig {
  target_weight?: number;
  step_weight?: number;
}
// bluegreen 的 config 形态。
interface BlueGreenConfig {
  blue_version_id?: string;
  green_version_id?: string;
}

export default function ReleasesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Release[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<VersionMeta[]>([]);

  // 策略筛选：空=全部。
  const [filter, setFilter] = useState<Strategy | ''>('');
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 新建弹窗。
  const [createOpen, setCreateOpen] = useState(false);
  const [strategy, setStrategy] = useState<Strategy>('canary');
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [name, setName] = useState('');
  const [stableId, setStableId] = useState('');
  const [canaryId, setCanaryId] = useState('');
  const [blueId, setBlueId] = useState('');
  const [greenId, setGreenId] = useState('');
  const [initialWeight, setInitialWeight] = useState(10);

  // 编辑弹窗（仅 canary：name/target/step）。
  const [editing, setEditing] = useState<Release | null>(null);
  const [editName, setEditName] = useState('');
  const [editTarget, setEditTarget] = useState('100');
  const [editStep, setEditStep] = useState('10');

  // 调权重弹窗（canary）。
  const [weightTarget, setWeightTarget] = useState<Release | null>(null);
  const [weightVal, setWeightVal] = useState('25');

  // 事件历史弹窗。
  const [eventsOf, setEventsOf] = useState<Release | null>(null);
  const [events, setEvents] = useState<ReleaseEvent[]>([]);

  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');

  const load = useCallback(async () => {
    const q = filter ? `?strategy=${filter}&limit=200` : '?limit=200';
    try {
      setRows(await api.get<Release[]>(`/api/admin/releases${q}`));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t, filter]);

  useEffect(() => {
    load();
    api.get<Service[]>('/api/admin/services?limit=200').then(setServices).catch(() => {});
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setStableId('');
    setCanaryId('');
    setBlueId('');
    setGreenId('');
    setVersions([]);
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
    setStableId('');
    setCanaryId('');
    setBlueId('');
    setGreenId('');
    try {
      const v = await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`);
      setVersions(v.map((x) => ({ ...x, deployment_id: deploymentId, service_id: svcId })));
    } catch { setVersions([]); }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    try {
      await api.post(`/api/admin/releases/${id}/${act}`, body);
      toast.success(t('releases.operateDone', { act }));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doCreate = async () => {
    setFormErr('');
    if (!name) {
      setFormErr(t('releases.errFill'));
      return;
    }
    const body: Record<string, unknown> = { strategy, service_id: svcId || undefined, name };
    if (strategy === 'canary') {
      if (!stableId || !canaryId) {
        setFormErr(t('releases.errFill'));
        return;
      }
      if (stableId === canaryId) {
        setFormErr(t('releases.sameVersion'));
        return;
      }
      body.primary_version_id = stableId;
      body.secondary_version_id = canaryId;
      body.initial_weight = initialWeight;
    } else {
      if (!blueId || !greenId) {
        setFormErr(t('releases.errPickDeploy'));
        return;
      }
      if (blueId === greenId) {
        setFormErr(t('releases.sameVersion'));
        return;
      }
      body.blue_version_id = blueId;
      body.green_version_id = greenId;
    }
    setBusy(true);
    try {
      await create('/api/admin/releases', body);
      toast.success(t('releases.createHint'));
      resetCreate();
      setCreateOpen(false);
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  const resetCreate = () => {
    setName('');
    setSvcId('');
    setDepId('');
    setStableId('');
    setCanaryId('');
    setBlueId('');
    setGreenId('');
    setVersions([]);
    setDeploys([]);
  };

  const openEdit = (r: Release) => {
    setEditing(r);
    setFormErr('');
    const cfg = (r.config ?? {}) as CanaryConfig;
    setEditName(r.name);
    setEditTarget(String(cfg.target_weight ?? 100));
    setEditStep(String(cfg.step_weight ?? 10));
  };

  const doEdit = async () => {
    if (!editing) return;
    setFormErr('');
    if (!editName) {
      setFormErr(t('releases.errFill'));
      return;
    }
    setBusy(true);
    try {
      await update('/api/admin/releases', editing.id, {
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

  const openWeight = (r: Release) => {
    setWeightTarget(r);
    setWeightVal(String(r.secondary_weight ?? 25));
  };

  const doWeight = async () => {
    if (!weightTarget) return;
    const n = Number(weightVal);
    if (Number.isNaN(n) || n < 0 || n > 100) {
      toast.error(t('releases.weightRange'));
      return;
    }
    setBusy(true);
    try {
      await api.post(`/api/admin/releases/${weightTarget.id}/weight`, { weight: n });
      toast.success(t('releases.operateDone', { act: 'weight' }));
      setWeightTarget(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    } finally {
      setBusy(false);
    }
  };

  const openEvents = async (r: Release) => {
    setEventsOf(r);
    setEvents([]);
    try {
      setEvents(await api.get<ReleaseEvent[]>(`/api/admin/releases/${r.id}/events`));
    } catch {
      setEvents([]);
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/releases', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const verName = (id: string) => versions.find((v) => v.id === id)?.version ?? shortId(id);
  const svcName = (id?: string | null) =>
    id ? services.find((s) => s.id === id)?.name ?? shortId(id) : '—';

  const cfgCanary = (r: Release) => (r.config ?? {}) as CanaryConfig;
  const cfgBG = (r: Release) => (r.config ?? {}) as BlueGreenConfig;

  // bluegreen：从 config 取 blue/green 两色，判断哪一色为 active（primary）。
  const bgActiveIsBlue = (r: Release) => cfgBG(r).blue_version_id === r.primary_version_id;

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  const filtered = useMemo(() => applyFilters(rows, RELEASE_FILTERS, filterValues), [rows, filterValues]);
  const setFilterValue = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  const filterBtn = (val: Strategy | '', label: string) => (
    <button
      onClick={() => setFilter(val)}
      className={`rounded px-3 py-1 text-sm ${
        filter === val
          ? 'bg-th-accent text-white'
          : 'bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600'
      }`}
    >
      {label}
    </button>
  );

  return (
    <div>
      <PageHeader
        title={t('releases.title')}
        right={
          <button
            onClick={() => {
              setFormErr('');
              setCreateOpen(true);
            }}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('releases.new')}`}
          </button>
        }
      />

      <FilterBar
        filters={RELEASE_FILTERS}
        values={filterValues}
        onChange={setFilterValue}
        onReset={resetFilter}
        rows={rows}
        parents={{ '/api/admin/services': services }}
      />

      <div className="mb-3 flex gap-2">
        {filterBtn('', t('releases.filterAll'))}
        {filterBtn('canary', t('releases.strategyCanary'))}
        {filterBtn('bluegreen', t('releases.strategyBlueGreen'))}
      </div>

      {/* 新建弹窗 */}
      <Modal
        open={createOpen}
        title={t('releases.new')}
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
        {formErr && <FormError msg={formErr} />}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('releases.strategy')}>
            <select value={strategy} onChange={(e) => setStrategy(e.target.value as Strategy)} className={inputCls}>
              <option value="canary">{t('releases.strategyCanary')}</option>
              <option value="bluegreen">{t('releases.strategyBlueGreen')}</option>
            </select>
          </Field>
          <Field label={t('fields.name')}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('releases.namePh')} className={inputCls} />
          </Field>
          <Field label={t('fields.service')}>
            <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className={inputCls}>
              <option value="">{t('releases.selectService')}</option>
              {services.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('fields.deploymentName')}>
            <select value={depId} disabled={!deploys.length} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
              <option value="">{t('releases.selectDeployment')}</option>
              {deploys.map((d) => (
                <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>
              ))}
            </select>
          </Field>

          {strategy === 'canary' ? (
            <>
              <Field label={t('releases.stableVersion')}>
                <select value={stableId} onChange={(e) => setStableId(e.target.value)} className={inputCls}>
                  <option value="">{t('releases.selectStable')}</option>
                  {versions.map((v) => (
                    <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                  ))}
                </select>
              </Field>
              <Field label={t('releases.canaryVersion')}>
                <select value={canaryId} onChange={(e) => setCanaryId(e.target.value)} className={inputCls}>
                  <option value="">{t('releases.selectCanary')}</option>
                  {versions.map((v) => (
                    <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                  ))}
                </select>
              </Field>
              <Field label={t('releases.initialWeight')}>
                <input type="number" min={1} max={100} value={initialWeight} onChange={(e) => setInitialWeight(Number(e.target.value))} className={inputCls} />
              </Field>
            </>
          ) : (
            <>
              <Field label={t('releases.blueVersion')}>
                <select value={blueId} onChange={(e) => setBlueId(e.target.value)} className={inputCls}>
                  <option value="">{t('releases.selectVersion')}</option>
                  {versions.map((v) => (
                    <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                  ))}
                </select>
              </Field>
              <Field label={t('releases.greenVersion')}>
                <select value={greenId} onChange={(e) => setGreenId(e.target.value)} className={inputCls}>
                  <option value="">{t('releases.selectVersion')}</option>
                  {versions.map((v) => (
                    <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                  ))}
                </select>
              </Field>
            </>
          )}
        </div>
      </Modal>

      {/* 编辑弹窗（仅 canary） */}
      <Modal
        open={editing !== null}
        title={t('releases.editTitle')}
        onClose={() => setEditing(null)}
        footer={
          <>
            <button
              onClick={() => setEditing(null)}
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
        {formErr && <FormError msg={formErr} />}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('fields.name')}>
            <input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder={t('releases.namePh')} className={inputCls} />
          </Field>
          <Field label={t('releases.targetWeight')}>
            <input type="number" min={1} max={100} value={editTarget} onChange={(e) => setEditTarget(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('releases.stepWeight')}>
            <input type="number" min={1} max={100} value={editStep} onChange={(e) => setEditStep(e.target.value)} className={inputCls} />
          </Field>
        </div>
        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('releases.ownershipHint')}</p>
      </Modal>

      {/* 调权重弹窗（canary） */}
      <Modal
        open={weightTarget !== null}
        title={t('releases.setWeightTitle')}
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
        <Field label={t('releases.weightLabel')}>
          <input type="number" min={0} max={100} value={weightVal} onChange={(e) => setWeightVal(e.target.value)} className={inputCls} />
        </Field>
      </Modal>

      {/* 事件历史弹窗 */}
      <Modal
        open={eventsOf !== null}
        title={t('releases.events')}
        onClose={() => setEventsOf(null)}
        footer={
          <button
            onClick={() => setEventsOf(null)}
            className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
          >
            {t('common.close')}
          </button>
        }
      >
        {events.length === 0 ? (
          <p className="text-sm text-slate-400 dark:text-slate-500">{t('releases.noEvents')}</p>
        ) : (
          <div className="max-h-80 overflow-y-auto">
            <table className="w-full text-sm">
              <thead className="text-left text-xs text-slate-500 dark:text-slate-400">
                <tr>
                  <th className="py-1 pr-3">{t('logs.time')}</th>
                  <th className="py-1 pr-3">{t('logs.type')}</th>
                  <th className="py-1">{t('fields.description')}</th>
                </tr>
              </thead>
              <tbody>
                {events.map((e) => (
                  <tr key={e.id} className="border-t border-slate-100 dark:border-slate-700">
                    <td className="py-1 pr-3 text-xs text-slate-500 dark:text-slate-400">{new Date(e.created_at).toLocaleString()}</td>
                    <td className="py-1 pr-3">{e.action}</td>
                    <td className="py-1 text-xs">{e.detail}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Modal>

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('releases.strategy')}</th>
              <th className="px-4 py-2">{t('fields.service')}</th>
              <th className="px-4 py-2">{t('fields.version')}</th>
              <th className="px-4 py-2">{t('fields.weight')}</th>
              <th className="px-4 py-2">{t('fields.phase')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr><td colSpan={7} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{rows.length === 0 ? t('releases.none') : t('common.noMatch')}</td></tr>
            )}
            {filtered.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">
                  <span className="text-xs">{r.strategy === 'canary' ? t('releases.strategyCanary') : t('releases.strategyBlueGreen')}</span>
                </td>
                <td className="px-4 py-2">{svcName(r.service_id)}</td>
                <td className="px-4 py-2 text-xs">
                  {r.strategy === 'canary'
                    ? `stable ${verName(r.primary_version_id)} → canary ${verName(r.secondary_version_id)}`
                    : `blue ${verName(cfgBG(r).blue_version_id ?? '')} / green ${verName(cfgBG(r).green_version_id ?? '')}`}
                </td>
                <td className="px-4 py-2 text-xs">
                  {r.strategy === 'canary'
                    ? `${r.secondary_weight}% / ${cfgCanary(r).target_weight ?? 100}%`
                    : `active ${verName(r.primary_version_id)}`}
                </td>
                <td className="px-4 py-2"><PhaseBadge phase={r.phase} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEvents(r)}>{t('releases.events')}</ActionBtn>
                    {r.strategy === 'canary' && (
                      <>
                        <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                        {r.phase === 'created' && (
                          <ActionBtn onClick={() => action(r.id, 'start')}>{t('releases.start')}</ActionBtn>
                        )}
                        {r.phase === 'running' && (
                          <>
                            <ActionBtn onClick={() => action(r.id, 'pause')}>{t('releases.pause')}</ActionBtn>
                            <ActionBtn onClick={() => openWeight(r)}>{t('releases.adjustWeight')}</ActionBtn>
                            <ActionBtn onClick={() => action(r.id, 'promote')}>{t('releases.promote')}</ActionBtn>
                            <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('releases.rollback')}</ActionBtn>
                          </>
                        )}
                        {r.phase === 'paused' && (
                          <>
                            <ActionBtn onClick={() => action(r.id, 'resume')}>{t('releases.resume')}</ActionBtn>
                            <ActionBtn onClick={() => openWeight(r)}>{t('releases.adjustWeight')}</ActionBtn>
                            <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('releases.rollback')}</ActionBtn>
                          </>
                        )}
                      </>
                    )}
                    {r.strategy === 'bluegreen' && (
                      <>
                        {bgActiveIsBlue(r) ? (
                          <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: cfgBG(r).green_version_id })}>{t('releases.switchToGreen')}</ActionBtn>
                        ) : (
                          <ActionBtn onClick={() => action(r.id, 'switch', { target_version_id: cfgBG(r).blue_version_id })}>{t('releases.switchToBlue')}</ActionBtn>
                        )}
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>{t('releases.rollback')}</ActionBtn>
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
        message={t('releases.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}

function FormError({ msg }: { msg: string }) {
  return (
    <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
      {msg}
    </p>
  );
}
