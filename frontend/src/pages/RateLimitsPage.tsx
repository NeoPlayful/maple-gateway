import { useCallback, useEffect, useState } from 'react';
import { api } from '../lib/client';
import { create, remove, statusAction } from '../lib/modules';
import type { Domain, RateLimit, Service, Tenant } from '../types';
import { StatusBadge, ActionBtn } from '../components/ui';

const scopes = [
  { value: 'global', label: '全局 Global' },
  { value: 'tenant', label: '租户 Tenant' },
  { value: 'domain', label: '域名 Domain' },
  { value: 'service', label: '服务 Service' },
  { value: 'ip', label: 'IP' },
];

export default function RateLimitsPage() {
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
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  const loadParents = useCallback(async () => {
    try {
      const [t, d, s] = await Promise.all([
        api.get<Tenant[]>('/api/admin/tenants?limit=200'),
        api.get<Domain[]>('/api/admin/domains?limit=200'),
        api.get<Service[]>('/api/admin/services?limit=200'),
      ]);
      setTenants(t);
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
      setMsg('操作成功');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '操作失败');
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
      setMsg('创建成功');
      setOpen(false);
      setName('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '创建失败');
    }
  };

  const inputCls = 'w-full rounded border px-2 py-1.5 text-sm';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">限流规则</h1>
        <button onClick={() => setOpen((v) => !v)} className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700">
          {open ? '收起' : '+ 新建限流规则'}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl bg-white p-5 shadow-sm">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <div>
              <label className="mb-1 block text-xs text-slate-500">维度 Scope</label>
              <select value={scope} onChange={(e) => { setScope(e.target.value); setTenantId(''); setDomainId(''); setServiceId(''); }} className={inputCls}>
                {scopes.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">名称</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 api 5/s" className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">配额 Limit</label>
              <input type="number" min={1} value={limit} onChange={(e) => setLimit(e.target.value)} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">窗口(秒)</label>
              <input type="number" min={1} value={windowSec} onChange={(e) => setWindowSec(e.target.value)} className={inputCls} />
            </div>
            {scope === 'tenant' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500">租户</label>
                <select value={tenantId} onChange={(e) => setTenantId(e.target.value)} className={inputCls}>
                  <option value="">选择租户</option>
                  {tenants.map((t) => <option key={t.id} value={t.id}>{t.name}</option>)}
                </select>
              </div>
            )}
            {scope === 'domain' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500">域名</label>
                <select value={domainId} onChange={(e) => setDomainId(e.target.value)} className={inputCls}>
                  <option value="">选择域名</option>
                  {domains.map((d) => <option key={d.id} value={d.id}>{d.hostname}</option>)}
                </select>
              </div>
            )}
            {scope === 'service' && (
              <div>
                <label className="mb-1 block text-xs text-slate-500">服务</label>
                <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className={inputCls}>
                  <option value="">选择服务</option>
                  {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                </select>
              </div>
            )}
            <div>
              <label className="mb-1 block text-xs text-slate-500">突发 Burst</label>
              <input type="number" value={burst} onChange={(e) => setBurst(e.target.value)} placeholder="可选" className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">超限状态码</label>
              <input type="number" value={code} onChange={(e) => setCode(e.target.value)} className={inputCls} />
            </div>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700">创建</button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
            <tr>
              <th className="px-4 py-2">名称</th>
              <th className="px-4 py-2">维度</th>
              <th className="px-4 py-2">对象</th>
              <th className="px-4 py-2">配额</th>
              <th className="px-4 py-2">状态</th>
              <th className="px-4 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-slate-400">暂无限流规则</td></tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b hover:bg-slate-50">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2 text-xs text-slate-500">{r.scope}</td>
                <td className="px-4 py-2 text-xs">{refName(r)}</td>
                <td className="px-4 py-2">
                  <span className="font-mono text-xs">{r.limit}</span>
                  <span className="text-slate-400">/ {r.window_seconds}s</span>
                  {r.burst ? <span className="ml-1 text-xs text-amber-600">burst {r.burst}</span> : null}
                </td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.status === 'disabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>启用</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>禁用</ActionBtn>}
                    <ActionBtn danger onClick={async () => { if (!window.confirm('确认删除该限流规则？')) return; await remove('/api/admin/rate-limits', r.id); await load(); }}>删除</ActionBtn>
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
