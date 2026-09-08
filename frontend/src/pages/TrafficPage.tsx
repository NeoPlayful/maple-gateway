import { useCallback, useEffect, useState } from 'react';
import { list, create, statusAction, remove } from '../lib/modules';
import type { TrafficPolicy, Service } from '../types';
import { StatusBadge, ActionBtn, Field } from '../components/ui';

export default function TrafficPage() {
  const [rows, setRows] = useState<TrafficPolicy[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

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
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  useEffect(() => {
    load();
    list<Service>('/api/admin/services').then(setServices).catch(() => {});
  }, [load]);

  const act = async (id: string, a: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction('/api/admin/traffic', id, a);
      setMsg('操作成功');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '操作失败');
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm('确认删除该策略？')) return;
    try {
      await remove('/api/admin/traffic', id);
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '删除失败');
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
    return parts.length ? parts.join(' ') : '权重兜底';
  };

  const submit = async () => {
    setErr('');
    if (!serviceId || !name) {
      setErr('请填写服务与策略名');
      return;
    }
    const match: Record<string, unknown> = {};
    if (headerKey && headerVal) match.header = { [headerKey]: headerVal };
    if (cookieKey && cookieVal) match.cookie = { [cookieKey]: cookieVal };
    if (path) match.path = path;
    if (pathExact) match.path_exact = pathExact;
    if (percent) match.percent = Number(percent);
    try {
      await create('/api/admin/traffic', {
        service_id: serviceId,
        name,
        priority: Number(priority) || 100,
        match,
        status: 'enabled',
      });
      setMsg('创建成功');
      setOpen(false);
      setName('');
      setHeaderKey('');
      setHeaderVal('');
      setCookieKey('');
      setCookieVal('');
      setPath('');
      setPathExact('');
      setPercent('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '创建失败');
    }
  };

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">流量策略</h1>
        <button onClick={() => setOpen((v) => !v)} className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700">
          {open ? '收起' : '+ 新建策略'}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl bg-white p-5 shadow-sm">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <Field label="服务">
              <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择服务</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </Field>
            <Field label="策略名">
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 canary-header" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="优先级 (小优先)">
              <input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="流量比例 % (0-100)">
              <input type="number" value={percent} onChange={(e) => setPercent(e.target.value)} placeholder="如 10" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Header 键">
              <input value={headerKey} onChange={(e) => setHeaderKey(e.target.value)} placeholder="如 x-canary" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Header 值">
              <input value={headerVal} onChange={(e) => setHeaderVal(e.target.value)} placeholder="如 beta" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Cookie 键">
              <input value={cookieKey} onChange={(e) => setCookieKey(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Cookie 值">
              <input value={cookieVal} onChange={(e) => setCookieVal(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Path 前缀">
              <input value={path} onChange={(e) => setPath(e.target.value)} placeholder="如 /api" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="Path 精确">
              <input value={pathExact} onChange={(e) => setPathExact(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700">
            创建
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
            <tr>
              <th className="px-4 py-2">名称</th>
              <th className="px-4 py-2">优先级</th>
              <th className="px-4 py-2">匹配</th>
              <th className="px-4 py-2">目标</th>
              <th className="px-4 py-2">状态</th>
              <th className="px-4 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400">暂无策略</td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b hover:bg-slate-50">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">{r.priority}</td>
                <td className="px-4 py-2 text-xs text-slate-600">{matchDesc(r)}</td>
                <td className="px-4 py-2 text-xs">{r.target_version_id ? r.target_version_id.slice(0, 8) : '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>启用</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>禁用</ActionBtn>}
                    <ActionBtn danger onClick={() => doDelete(r.id)}>删除</ActionBtn>
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
