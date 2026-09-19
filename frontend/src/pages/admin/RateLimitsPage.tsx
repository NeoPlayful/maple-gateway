import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import { create, update, remove, statusAction } from '../../lib/modules';
import type { Domain, RateLimit, Service, Tenant } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

const scopes = [
  { value: 'global', labelKey: 'ratelimit.scopeGlobal' },
  { value: 'tenant', labelKey: 'ratelimit.scopeTenant' },
  { value: 'domain', labelKey: 'ratelimit.scopeDomain' },
  { value: 'service', labelKey: 'ratelimit.scopeService' },
  { value: 'ip', labelKey: 'ratelimit.scopeIp' },
];

// 筛选栏：关键字（规则名）+ 维度（scope）+ 状态，纯前端过滤已加载列表。
const RATELIMIT_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name'], placeholder: 'ratelimit.filterPh' },
  { type: 'select', key: 'scope', placeholder: 'common.filterAllScope', options: scopes.map((s) => ({ value: s.value, label: s.labelKey })) },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
];

export default function RateLimitsPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<RateLimit[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 表单弹窗：editing 非空=编辑，空=新建。
  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<RateLimit | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  // 待删除规则 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const [scope, setScope] = useState('service');
  const [tenantId, setTenantId] = useState('');
  const [domainId, setDomainId] = useState('');
  const [serviceId, setServiceId] = useState('');
  const [name, setName] = useState('');
  const [limit, setLimit] = useState('100');
  const [windowSec, setWindowSec] = useState('60');
  const [burst, setBurst] = useState('');
  const [code, setCode] = useState('429');

  const load = useCallback(async () => {
    try {
      setRows(await api.get<RateLimit[]>('/api/admin/rate-limits?limit=200'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  const loadParents = useCallback(async () => {
    try {
      const [tt, d, s] = await Promise.all([
        api.get<Tenant[]>('/api/admin/tenants?limit=200'),
        api.get<Domain[]>('/api/admin/domains?limit=200'),
        api.get<Service[]>('/api/admin/services?limit=200'),
      ]);
      setTenants(tt);
      setDomains(d);
      setServices(s);
    } catch {
      /* 元数据加载失败忽略 */
    }
  }, []);

  useEffect(() => {
    load();
    loadParents();
  }, [load, loadParents]);

  const refName = (r: RateLimit) => {
    if (r.scope === 'tenant') return tenants.find((x) => x.id === r.tenant_id)?.name ?? shortId(r.tenant_id);
    if (r.scope === 'domain') return domains.find((x) => x.id === r.domain_id)?.hostname ?? shortId(r.domain_id);
    if (r.scope === 'service') return services.find((x) => x.id === r.service_id)?.name ?? shortId(r.service_id);
    return '—';
  };

  const act = async (id: string, a: string) => {
    try {
      await statusAction('/api/admin/rate-limits', id, a);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/rate-limits', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  // 打开新建弹窗：清空表单。
  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setScope('service');
    setTenantId('');
    setDomainId('');
    setServiceId('');
    setName('');
    setLimit('100');
    setWindowSec('60');
    setBurst('');
    setCode('429');
    setFormOpen(true);
  };

  // 打开编辑弹窗：回填可改字段；scope 与目标只读（后端不支持改）。
  const openEdit = (r: RateLimit) => {
    setEditing(r);
    setFormErr('');
    setScope(r.scope);
    setTenantId(r.tenant_id ?? '');
    setDomainId(r.domain_id ?? '');
    setServiceId(r.service_id ?? '');
    setName(r.name);
    setLimit(String(r.limit));
    setWindowSec(String(r.window_seconds));
    setBurst(r.burst ? String(r.burst) : '');
    setCode(String(r.response_code ?? 429));
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  const submit = async () => {
    setFormErr('');
    if (!name || !limit) {
      setFormErr(t('ratelimit.errFill'));
      return;
    }
    setBusy(true);
    try {
      if (editing) {
        // 编辑仅改运行参数；scope 与目标经创建后不可改，此处不动。
        const body: Record<string, unknown> = {
          name,
          limit: Number(limit),
          window_seconds: Number(windowSec),
          response_code: Number(code),
        };
        if (burst !== '') body.burst = Number(burst);
        await update('/api/admin/rate-limits', editing.id, body);
        toast.success(t('common.updateSuccess'));
      } else {
        const body: Record<string, unknown> = {
          scope,
          name,
          limit: Number(limit),
          window_seconds: Number(windowSec),
          response_code: Number(code),
        };
        if (burst !== '') body.burst = Number(burst);
        if (scope === 'tenant' && tenantId) body.tenant_id = tenantId;
        if (scope === 'domain' && domainId) body.domain_id = domainId;
        if (scope === 'service' && serviceId) body.service_id = serviceId;
        await create('/api/admin/rate-limits', body);
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

  const filtered = useMemo(() => applyFilters(rows, RATELIMIT_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  return (
    <div>
      <PageHeader
        title={t('ratelimit.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {`+ ${t('ratelimit.newRule')}`}
          </button>
        }
      />

      <Modal
        open={formOpen}
        title={editing ? t('ratelimit.editTitle') : t('ratelimit.newRule')}
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
          <Field label={t('ratelimit.scope')}>
            <select
              value={scope}
              disabled={!!editing}
              onChange={(e) => { setScope(e.target.value); setTenantId(''); setDomainId(''); setServiceId(''); }}
              className={inputCls}
            >
              {scopes.map((s) => <option key={s.value} value={s.value}>{t(s.labelKey)}</option>)}
            </select>
          </Field>
          <Field label={t('fields.name')}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('ratelimit.namePh')} className={inputCls} />
          </Field>
          {scope === 'tenant' && (
            <Field label={t('fields.tenant')}>
              <select value={tenantId} disabled={!!editing} onChange={(e) => setTenantId(e.target.value)} className={inputCls}>
                <option value="">{t('ratelimit.selectTenant')}</option>
                {tenants.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
              </select>
            </Field>
          )}
          {scope === 'domain' && (
            <Field label={t('fields.hostname')}>
              <select value={domainId} disabled={!!editing} onChange={(e) => setDomainId(e.target.value)} className={inputCls}>
                <option value="">{t('ratelimit.selectDomain')}</option>
                {domains.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
              </select>
            </Field>
          )}
          {scope === 'service' && (
            <Field label={t('fields.service')}>
              <select value={serviceId} disabled={!!editing} onChange={(e) => setServiceId(e.target.value)} className={inputCls}>
                <option value="">{t('ratelimit.selectService')}</option>
                {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
              </select>
            </Field>
          )}
          <Field label={t('ratelimit.limit')}>
            <input type="number" min={1} value={limit} onChange={(e) => setLimit(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('ratelimit.windowSec')}>
            <input type="number" min={1} value={windowSec} onChange={(e) => setWindowSec(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('ratelimit.burst')}>
            <input type="number" value={burst} onChange={(e) => setBurst(e.target.value)} placeholder={t('ratelimit.optional')} className={inputCls} />
          </Field>
          <Field label={t('ratelimit.responseCode')}>
            <input type="number" value={code} onChange={(e) => setCode(e.target.value)} className={inputCls} />
          </Field>
        </div>
        {editing && (
          <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.ownershipHint')}</p>
        )}
      </Modal>

      <FilterBar
        filters={RATELIMIT_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('ratelimit.scope')}</th>
              <th className="px-4 py-2">{t('ratelimit.target')}</th>
              <th className="px-4 py-2">{t('ratelimit.limit')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{rows.length === 0 ? t('ratelimit.none') : t('common.noMatch')}</td></tr>
            )}
            {filtered.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2 text-xs text-slate-500">{t(`ratelimit.scopeValue.${r.scope}`, { defaultValue: r.scope })}</td>
                <td className="px-4 py-2 text-xs">{refName(r)}</td>
                <td className="px-4 py-2">
                  <span className="font-mono text-xs">{r.limit}</span>
                  <span className="text-slate-400">/ {r.window_seconds}s</span>
                  {r.burst ? <span className="ml-1 text-xs text-amber-600">burst {r.burst}</span> : null}
                </td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    {r.status === 'disabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
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
        message={t('ratelimit.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
