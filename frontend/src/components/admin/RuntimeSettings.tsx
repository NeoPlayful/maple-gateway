// 数据面运行时配置（health / proxy）的结构化表单。
// 值存于 settings 表对应 section，缺行时后端回退内建默认（此处按默认占位显示）。
// 每项标注生效方式：热（改完即生效）/ 温（下一轮生效）/ 重启（需重启进程）。
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';

const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 font-mono text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

// Effect 表示配置项的生效方式，决定表单里的徽标文案。
type Effect = 'hot' | 'warm' | 'restart';

interface FieldDef {
  key: string;
  effect: Effect;
  // duration=true 表示以时长字符串（如 "10s"）存放。
  duration?: boolean;
  def: string;
}

// 与后端 settings.Default*Runtime 及 config.Default 对齐。
const HEALTH_FIELDS: FieldDef[] = [
  { key: 'interval', effect: 'warm', duration: true, def: '10s' },
  { key: 'timeout', effect: 'warm', duration: true, def: '3s' },
  { key: 'failure_threshold', effect: 'hot', def: '3' },
  { key: 'success_threshold', effect: 'hot', def: '2' },
  { key: 'grace_period', effect: 'hot', duration: true, def: '10s' },
];

const PROXY_FIELDS: FieldDef[] = [
  { key: 'read_header_timeout', effect: 'restart', duration: true, def: '10s' },
  { key: 'read_timeout', effect: 'restart', duration: true, def: '60s' },
  { key: 'response_header_timeout', effect: 'restart', duration: true, def: '60s' },
  { key: 'idle_timeout', effect: 'restart', duration: true, def: '120s' },
  { key: 'max_header_bytes', effect: 'restart', def: '1048576' },
  { key: 'max_body_bytes', effect: 'hot', def: '10485760' },
  { key: 'max_conns_per_host', effect: 'restart', def: '512' },
  { key: 'max_idle_conns', effect: 'restart', def: '512' },
  { key: 'max_idle_conns_per_host', effect: 'restart', def: '128' },
  { key: 'max_in_flight', effect: 'hot', def: '4096' },
];

type KV = { value: unknown; version: number; updated_at: string };
type SectionMap = Record<string, KV>;

// effectCls 徽标配色：热=绿，温=琥珀，重启=灰。
function effectCls(e: Effect): string {
  switch (e) {
    case 'hot':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300';
    case 'warm':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300';
    default:
      return 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300';
  }
}

export default function RuntimeSettings({ section }: { section: 'health' | 'proxy' }) {
  const { t } = useTranslation('admin');
  const fields = section === 'health' ? HEALTH_FIELDS : PROXY_FIELDS;
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [meta, setMeta] = useState<SectionMap>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');

  const load = async () => {
    try {
      const d = await api.get<Record<string, SectionMap>>('/api/admin/settings');
      const sec = d[section] ?? {};
      setMeta(sec);
      const m: Record<string, string> = {};
      for (const f of fields) {
        const v = sec[f.key]?.value;
        // 缺行按内建默认占位；时长值原样呈现为 "10s" 字符串。
        m[f.key] = v === undefined || v === null ? f.def : String(v);
      }
      setDraft(m);
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [section]);

  const save = async () => {
    setErr('');
    setBusy(true);
    try {
      // 后端按键类型解析：时长传字符串、数值传数字。
      const body: Record<string, unknown> = {};
      for (const f of fields) {
        const raw = draft[f.key];
        if (raw === undefined || raw === '') continue;
        body[f.key] = f.duration ? raw : Number(raw);
      }
      await api.patch(`/api/admin/settings/${section}`, body);
      toast.success(t('settings.saved'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('settings.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      {err && (
        <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
          {err}
        </p>
      )}
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        {fields.map((f) => (
          <div key={f.key} className="flex flex-col gap-1">
            <label className="flex items-center gap-2 text-sm text-slate-600 dark:text-slate-300">
              {t(`settings.runtime.${f.key}`, f.key)}
              <span className={`rounded px-1.5 py-0.5 text-xs ${effectCls(f.effect)}`}>
                {t(`settings.runtime.effect.${f.effect}`)}
              </span>
              <span className="text-xs text-slate-400">v{meta[f.key]?.version ?? 1}</span>
            </label>
            <input
              value={draft[f.key] ?? ''}
              onChange={(e) => setDraft((m) => ({ ...m, [f.key]: e.target.value }))}
              placeholder={f.def}
              className={inputCls}
            />
          </div>
        ))}
      </div>
      <div className="mt-4 flex gap-3">
        <button
          onClick={save}
          disabled={busy}
          className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-50"
        >
          {busy ? t('common.saving') : t('common.save')}
        </button>
        <button
          onClick={load}
          className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
        >
          {t('common.refresh')}
        </button>
      </div>
    </div>
  );
}
