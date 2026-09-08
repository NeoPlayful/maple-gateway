import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import { create, remove, statusAction } from '../../lib/modules';
import type { Domain, RateLimit, Service, Tenant } from '../../types';
import { StatusBadge, ActionBtn } from '../../components/ui';

const scopes = [
  { value: 'global', labelKey: 'ratelimit.scopeGlobal' },
  { value: 'tenant', labelKey: 'ratelimit.scopeTenant' },
  { value: 'domain', labelKey: 'ratelimit.scopeDomain' },
  { value: 'service', labelKey: 'ratelimit.scopeService' },
  { value: 'ip', labelKey: 'ratelimit.scopeIp' },
];

export default function RateLimitsPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<RateLimit[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  // 创建表单。
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
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
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
    if (r.scope === 'tenant') return tenants.find((x) => x.id === r.tenant_id)?.name ?? r.tenant_id?.slice(0, 8);
    if (r.scope === 'domain') return domains.find((x) => x.id === r.domain_id)?.hostname ?? r.domain_id?.slice(0, 8);
    if (r.scope === 'service') return services.find((x) => x.id === r.service_id)?.name ?? r.service_id?.slice(0, 8);
    return '—';
  };

  const act = async (id: string, a: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction('/api/admin/rate-limits', id, a);
      setMsg(t('common.operateSuccess'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const submit = async () => {
    setErr('');
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
    try {
      await create('/api/admin/rate-limits', body);
      setMsg(t('common.createSuccess'));
      setOpen(false);
      setName('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed'));
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('ratelimit.title')}</h1>
        <button onClick={() => setOpen((v) => !v)} className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700 dark:bg-emerald-600 dark:hover:bg-emerald-700">
          {open ? t('common.collapse') : `+ ${t('ratelimit.newRule')}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.scope')}</label>
              <select value={scope} onChange={(e) => { setScope(e.target.value); setTenantId(''); setDomainId(''); setServiceId(''); }} className={inputCls}>
                {scopes.map((s) => <option key={s.value} value={s.value}>{t(s.labelKey)}</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.name')}</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('ratelimit.namePh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.limit')}</label>
              <input type="number" min={1} value={limit} onChange={(e) => setLimit(e.target.value)} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.windowSec')}</label>
              <input type="number" min={1} value={windowSec} onChange={(e) => setWindowSec(e.target.value)} className={inputCls} />
            </div>
            {scope === 'tenant' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.tenant')}</label>
                <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} className={inputCls}>
                  <option value="">{t('ratelimit.selectTenant')}</option>
                  {tenants.map((x) => <option key={x.id} value={x.id}>{x.name}</option>)}
                </select>
              </div>
            )}
            {scope === 'domain' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.hostname')}</label>
                <select value={domainId} onChange={(e) => setDomainId(e.target.value)} className={inputCls}>
                  <option value="">{t('ratelimit.selectDomain')}</option>
                  {domains.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
                </select>
              </div>
            )}
            {scope === 'service' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.service')}</label>
                <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className={inputCls}>
                  <option value="">{t('ratelimit.selectService')}</option>
                  {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                </select>
              </div>
            )}
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.burst')}</label>
              <input type="number" value={burst} onChange={(e) => setBurst(e.target.value)} placeholder={t('ratelimit.optional')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('ratelimit.responseCode')}</label>
              <input type="number" value={code} onChange={(e) => setCode(e.target.value)} className={inputCls} />
            </div>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:hover:bg-slate-600">{t('common.create')}</button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
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
            {rows.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('ratelimit.none')}</td></tr>
            )}
            {rows.map((r) => (
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
                    {r.status === 'disabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>}
                    <ActionBtn danger onClick={async () => { if (!window.confirm(t('ratelimit.confirmDelete'))) return; await remove('/api/admin/rate-limits', r.id); await load(); }}>{t('common.delete')}</ActionBtn>
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
