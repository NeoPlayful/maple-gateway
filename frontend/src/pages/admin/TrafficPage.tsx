import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { list, create, update, statusAction, remove } from '../../lib/modules';
import type { TrafficPolicy, Service } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

// 筛选栏：关键字（策略名）+ 服务 + 状态，纯前端过滤已加载列表。
const TRAFFIC_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name'], placeholder: 'traffic.filterPh' },
  { type: 'select', key: 'service_id', placeholder: 'common.filterAllService', loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' } },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
];

export default function TrafficPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<TrafficPolicy[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});
  // 待删除策略 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 表单弹窗：editing 非空=编辑，空=新建。
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<TrafficPolicy | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');

  const [serviceId, setServiceId] = useState('');
  const [name, setName] = useState('');
  const [priority, setPriority] = useState('100');
  const [headerKey, setHeaderKey] = useState('');
  const [headerVal, setHeaderVal] = useState('');
  const [cookieKey, setCookieKey] = useState('');
  const [cookieVal, setCookieVal] = useState('');
  const [path, setPath] = useState('');
  const [pathExact, setPathExact] = useState('');
  const [percent, setPercent] = useState('');

  const load = useCallback(async () => {
    try {
      setRows(await list('/api/admin/traffic'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    list<Service>('/api/admin/services').then(setServices).catch(() => {});
  }, [load]);

  const act = async (id: string, a: string) => {
    try {
      await statusAction('/api/admin/traffic', id, a);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/traffic', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const matchDesc = (r: TrafficPolicy) => {
    const m = r.match ?? {};
    const parts: string[] = [];
    if (m.header) Object.entries(m.header).forEach(([k, v]) => parts.push(`header:${k}=${v}`));
    if (m.cookie) Object.entries(m.cookie).forEach(([k, v]) => parts.push(`cookie:${k}=${v}`));
    if (m.path) parts.push(`path:${m.path}`);
    if (m.path_exact) parts.push(`exact:${m.path_exact}`);
    if (m.percent) parts.push(`${m.percent}%`);
    return parts.length ? parts.join(' ') : t('traffic.fallbackWeight');
  };

  // 打开新建弹窗：清空表单。
  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setServiceId('');
    setName('');
    setPriority('100');
    setHeaderKey('');
    setHeaderVal('');
    setCookieKey('');
    setCookieVal('');
    setPath('');
    setPathExact('');
    setPercent('');
    setFormOpen(true);
  };

  // 打开编辑弹窗：回填可改字段（match 取首条 header/cookie 键值）；service 只读。
  const openEdit = (r: TrafficPolicy) => {
    const m = r.match ?? {};
    setEditing(r);
    setFormErr('');
    setServiceId(r.service_id);
    setName(r.name);
    setPriority(String(r.priority ?? 100));
    const hk = m.header ? Object.entries(m.header)[0] : undefined;
    const ck = m.cookie ? Object.entries(m.cookie)[0] : undefined;
    setHeaderKey(hk?.[0] ?? '');
    setHeaderVal(hk?.[1] ?? '');
    setCookieKey(ck?.[0] ?? '');
    setCookieVal(ck?.[1] ?? '');
    setPath(m.path ?? '');
    setPathExact(m.path_exact ?? '');
    setPercent(m.percent ? String(m.percent) : '');
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  // 按表单字段重建 match（后端 PATCH 为整块替换语义）。
  const buildMatch = (): Record<string, unknown> => {
    const match: Record<string, unknown> = {};
    if (headerKey && headerVal) match.header = { [headerKey]: headerVal };
    if (cookieKey && cookieVal) match.cookie = { [cookieKey]: cookieVal };
    if (path) match.path = path;
    if (pathExact) match.path_exact = pathExact;
    if (percent) match.percent = Number(percent);
    return match;
  };

  const submit = async () => {
    setFormErr('');
    if (!serviceId || !name) {
      setFormErr(t('traffic.errFill'));
      return;
    }
    setBusy(true);
    try {
      const match = buildMatch();
      if (editing) {
        // 编辑仅改运行参数；所属服务经 POST 创建后不可改，此处不动。
        await update('/api/admin/traffic', editing.id, {
          name,
          priority: Number(priority) || 100,
          match,
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await create('/api/admin/traffic', {
          service_id: serviceId,
          name,
          priority: Number(priority) || 100,
          match,
          status: 'enabled',
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

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  const filtered = useMemo(() => applyFilters(rows, TRAFFIC_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  return (
    <div>
      <PageHeader
        title={t('traffic.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('traffic.newPolicy')}`}
          </button>
        }
      />

      <Modal
        open={formOpen}
        title={editing ? t('traffic.editTitle') : t('traffic.newPolicy')}
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
          <Field label={t('fields.service')}>
            <select
              value={serviceId}
              disabled={!!editing}
              onChange={(e) => setServiceId(e.target.value)}
              className={inputCls}
            >
              <option value="">{t('ratelimit.selectService')}</option>
              {services.map((s) => (
                <option key={s.id} value={s.id}>{s.name}</option>
              ))}
            </select>
          </Field>
          <Field label={t('traffic.policyName')}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('traffic.namePh')} className={inputCls} />
          </Field>
          <Field label={t('traffic.priority')}>
            <input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('traffic.percent')}>
            <input type="number" value={percent} onChange={(e) => setPercent(e.target.value)} placeholder={t('traffic.percentPh')} className={inputCls} />
          </Field>
          <Field label={t('traffic.headerKey')}>
            <input value={headerKey} onChange={(e) => setHeaderKey(e.target.value)} placeholder={t('traffic.headerKeyPh')} className={inputCls} />
          </Field>
          <Field label={t('traffic.headerValue')}>
            <input value={headerVal} onChange={(e) => setHeaderVal(e.target.value)} placeholder={t('traffic.headerValuePh')} className={inputCls} />
          </Field>
          <Field label={t('traffic.cookieKey')}>
            <input value={cookieKey} onChange={(e) => setCookieKey(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('traffic.cookieValue')}>
            <input value={cookieVal} onChange={(e) => setCookieVal(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('traffic.pathPrefix')}>
            <input value={path} onChange={(e) => setPath(e.target.value)} placeholder={t('traffic.pathPh')} className={inputCls} />
          </Field>
          <Field label={t('traffic.pathExact')}>
            <input value={pathExact} onChange={(e) => setPathExact(e.target.value)} className={inputCls} />
          </Field>
        </div>
        {editing && (
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('traffic.ownershipHint')}</p>
        )}
      </Modal>

      <FilterBar
        filters={TRAFFIC_FILTERS}
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
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('traffic.priority')}</th>
              <th className="px-4 py-2">{t('traffic.match')}</th>
              <th className="px-4 py-2">{t('traffic.target')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{rows.length === 0 ? t('traffic.none') : t('common.noMatch')}</td>
              </tr>
            )}
            {filtered.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">{r.priority}</td>
                <td className="px-4 py-2 text-xs text-slate-600 dark:text-slate-300">{matchDesc(r)}</td>
                <td className="px-4 py-2 text-xs">{r.target_version_id ? shortId(r.target_version_id) : '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>}
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
        message={t('traffic.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
