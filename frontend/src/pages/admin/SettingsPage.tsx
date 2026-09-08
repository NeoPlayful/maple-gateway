import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';

type KV = { value: unknown; version: number; updated_at: string };
type SettingsMap = Record<string, Record<string, KV>>;

const sections = ['gateway', 'proxy', 'health', 'security', 'logging', 'metrics'];

export default function SettingsPage() {
  const { t } = useTranslation('admin');
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
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
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
      setMsg(t('settings.saved'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('settings.saveFailed'));
    }
  };

  return (
    <div>
      <h1 className="mb-4 text-xl font-semibold text-slate-800 dark:text-slate-100">{t('settings.title')}</h1>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}
      <div className="mb-4 flex flex-wrap gap-2">
        {sections.map((s) => (
          <button
            key={s}
            onClick={() => setSection(s)}
            className={`rounded px-3 py-1.5 text-sm ${
              section === s
                ? 'bg-slate-800 text-white dark:bg-slate-600'
                : 'bg-slate-200 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600'
            }`}
          >
            {s}
          </button>
        ))}
      </div>
      <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
        {Object.keys(current).length === 0 && (
          <p className="py-4 text-sm text-slate-400 dark:text-slate-500">{t('settings.emptyHint')}</p>
        )}
        {Object.entries(draft).map(([k, v]) => (
          <div key={k} className="mb-3 flex flex-col items-start gap-1 sm:flex-row sm:items-center sm:gap-3">
            <label className="w-56 shrink-0 text-sm text-slate-600 dark:text-slate-300">{k}
              <span className="ml-1 text-xs text-slate-400">v{current[k]?.version ?? 1}</span>
            </label>
            <input
              value={v}
              onChange={(e) => setDraft((m) => ({ ...m, [k]: e.target.value }))}
              className="flex-1 rounded border border-slate-300 bg-white px-2 py-1.5 font-mono text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
            />
          </div>
        ))}
        <div className="mt-4 flex gap-3">
          <button onClick={save} className="rounded bg-emerald-600 px-4 py-1.5 text-sm text-white hover:bg-emerald-700 dark:bg-emerald-600 dark:hover:bg-emerald-700">
            {t('common.save')}
          </button>
          <button onClick={load} className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600">
            {t('common.refresh')}
          </button>
        </div>
      </div>
    </div>
  );
}
