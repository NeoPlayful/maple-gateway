import { useEffect, useState } from 'react';
import { api } from '../lib/client';

type KV = { value: unknown; version: number; updated_at: string };
type SettingsMap = Record<string, Record<string, KV>>;

const sections = ['gateway', 'proxy', 'health', 'security', 'logging', 'metrics'];

export default function SettingsPage() {
  const [data, setData] = useState<SettingsMap>({});
  const [section, setSection] = useState('proxy');
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  const load = async () => {
    try {
      const d = await api.get<SettingsMap>('/api/admin/settings');
      setData(d);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  };

  useEffect(() => {
    load();
  }, []);

  const current = data[section] ?? {};
  useEffect(() => {
    const m: Record<string, string> = {};
    for (const [k, v] of Object.entries(current)) {
      m[k] = typeof v.value === 'object' ? JSON.stringify(v.value) : String(v.value);
    }
    setDraft(m);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [section, data]);

  const save = async () => {
    setErr('');
    setMsg('');
    const body: Record<string, unknown> = {};
    for (const [k, raw] of Object.entries(draft)) {
      if (raw === '') continue;
      // 尝试解析 JSON 数字/对象；失败按字符串保存。
      const n = Number(raw);
      if (raw.trim() !== '' && !Number.isNaN(n)) body[k] = n;
      else {
        try { body[k] = JSON.parse(raw); } catch { body[k] = raw; }
      }
    }
    try {
      await api.patch(`/api/admin/settings/${section}`, body);
      setMsg('已保存并热生效');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '保存失败');
    }
  };

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold text-slate-800">系统设置</h1>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}
      <div className="mb-4 flex gap-2">
        {sections.map((s) => (
          <button
            key={s}
            onClick={() => setSection(s)}
            className={`rounded px-3 py-1.5 text-sm ${section === s ? 'bg-slate-800 text-white' : 'bg-slate-200 text-slate-600'}`}
          >
            {s}
          </button>
        ))}
      </div>
      <div className="rounded-xl bg-white p-5 shadow-sm">
        {Object.keys(current).length === 0 && (
          <p className="py-4 text-sm text-slate-400">该 section 暂无配置，填写下方并保存以新增。</p>
        )}
        {Object.entries(draft).map(([k, v]) => (
          <div key={k} className="mb-3 flex items-center gap-3">
            <label className="w-56 shrink-0 text-sm text-slate-600">{k}
              <span className="ml-1 text-xs text-slate-400">v{current[k]?.version ?? 1}</span>
            </label>
            <input
              value={v}
              onChange={(e) => setDraft((m) => ({ ...m, [k]: e.target.value }))}
              className="flex-1 rounded border px-2 py-1.5 font-mono text-sm"
            />
          </div>
        ))}
        <div className="mt-4 flex gap-3">
          <button onClick={save} className="rounded bg-emerald-600 px-4 py-1.5 text-sm text-white hover:bg-emerald-700">
            保存
          </button>
          <button onClick={load} className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-600 hover:bg-slate-300">
            刷新
          </button>
        </div>
      </div>
    </div>
  );
}
