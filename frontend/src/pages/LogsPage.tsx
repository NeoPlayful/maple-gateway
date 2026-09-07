import { useCallback, useState } from 'react';
import { api } from '../api/client';
import type { AccessLogRow, ErrorLogRow, AuditLogRow } from '../types';

type Tab = 'access' | 'error' | 'audit';

export default function LogsPage() {
  const [tab, setTab] = useState<Tab>('access');
  const [rows, setRows] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [host, setHost] = useState('');
  const [status, setStatus] = useState('');
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    setErr('');
    const qs = new URLSearchParams({ limit: '200' });
    if (host) qs.set('host', host);
    if (status) qs.set('status', status);
    try {
      const path = `/api/admin/logs/${tab}?${qs.toString()}`;
      const resp = await fetch(path, {
        headers: { Authorization: `Bearer ${localStorage.getItem('maple_token') ?? ''}` },
      });
      const j = await resp.json();
      if (j.code !== 'OK') {
        setErr(j.message || '加载失败');
        return;
      }
      setRows((j.data ?? []) as any[]);
      setTotal(Number(j.meta?.total ?? 0));
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, [tab, host, status]);

  const refresh = () => load();

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">日志查询</h1>
        <div className="flex gap-1">
          {(
            [
              ['access', '访问'],
              ['error', '错误'],
              ['audit', '审计'],
            ] as [Tab, string][]
          ).map(([t, label]) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`rounded px-3 py-1.5 text-sm ${tab === t ? 'bg-slate-800 text-white' : 'bg-slate-200 text-slate-600'}`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      <div className="mb-4 flex flex-wrap items-end gap-3 rounded-xl bg-white p-4 shadow-sm">
        <div>
          <label className="mb-1 block text-xs text-slate-500">Host</label>
          <input value={host} onChange={(e) => setHost(e.target.value)} placeholder="留空全部" className="w-48 rounded border px-2 py-1.5 text-sm" />
        </div>
        <div>
          <label className="mb-1 block text-xs text-slate-500">状态码</label>
          <input value={status} onChange={(e) => setStatus(e.target.value)} placeholder="如 200/404" className="w-24 rounded border px-2 py-1.5 text-sm" />
        </div>
        <button onClick={refresh} className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700">
          查询
        </button>
        <span className="ml-auto text-xs text-slate-400">共 {total} 条（内存缓冲最新 {tab === 'audit' ? '落库' : '200'} 条）</span>
      </div>

      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        {tab === 'access' && (
          <table className="w-full text-sm">
            <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
              <tr>
                <th className="px-4 py-2">时间</th>
                <th className="px-4 py-2">Host</th>
                <th className="px-4 py-2">Method</th>
                <th className="px-4 py-2">Path</th>
                <th className="px-4 py-2">状态</th>
                <th className="px-4 py-2">IP</th>
                <th className="px-4 py-2">耗时ms</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AccessLogRow[]).map((r, i) => (
                <tr key={i} className="border-b hover:bg-slate-50">
                  <td className="px-4 py-1.5 text-xs text-slate-500">{new Date(r.timestamp).toLocaleTimeString()}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.host}</td>
                  <td className="px-4 py-1.5">{r.method}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.path}</td>
                  <td className="px-4 py-1.5">{r.status}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.client_ip}</td>
                  <td className="px-4 py-1.5">{r.duration_ms}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {tab === 'error' && (
          <table className="w-full text-sm">
            <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
              <tr>
                <th className="px-4 py-2">时间</th>
                <th className="px-4 py-2">Host</th>
                <th className="px-4 py-2">Path</th>
                <th className="px-4 py-2">状态</th>
                <th className="px-4 py-2">错误</th>
              </tr>
            </thead>
            <tbody>
              {(rows as ErrorLogRow[]).map((r, i) => (
                <tr key={i} className="border-b hover:bg-slate-50">
                  <td className="px-4 py-1.5 text-xs text-slate-500">{new Date(r.timestamp).toLocaleTimeString()}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.host}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.path}</td>
                  <td className="px-4 py-1.5">{r.status}</td>
                  <td className="px-4 py-1.5 text-xs text-rose-600">{r.error}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {tab === 'audit' && (
          <table className="w-full text-sm">
            <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
              <tr>
                <th className="px-4 py-2">时间</th>
                <th className="px-4 py-2">Action</th>
                <th className="px-4 py-2">类型</th>
                <th className="px-4 py-2">对象</th>
                <th className="px-4 py-2">IP</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AuditLogRow[]).map((r) => (
                <tr key={r.id} className="border-b hover:bg-slate-50">
                  <td className="px-4 py-1.5 text-xs text-slate-500">{new Date(r.created_at).toLocaleString()}</td>
                  <td className="px-4 py-1.5">{r.action}</td>
                  <td className="px-4 py-1.5">{r.target_type}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.target_id?.slice(0, 8) ?? '-'}</td>
                  <td className="px-4 py-1.5 font-mono text-xs">{r.ip ?? '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {rows.length === 0 && <p className="px-4 py-8 text-center text-sm text-slate-400">暂无日志</p>}
      </div>
    </div>
  );
}
