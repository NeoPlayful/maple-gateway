// 状态徽章：按状态枚举映射配色，文案取 i18n 词表 status.<key>。
import { useTranslation } from 'react-i18next';

const color: Record<string, string> = {
  // 资源状态
  active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  enabled: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  online: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  healthy: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  // 中间态
  pending: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  draining: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  recovering: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  paused: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  // 异常
  suspended: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  disabled: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  offline: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  maintenance: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  unknown: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  unhealthy: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  failed: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  rolled_back: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  // 证书状态（Direct TLS）
  error: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  expiring: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  expired: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  // 签发/续期进度阶段（进行中）
  queued: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
  account_ready: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  order_created: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  challenge_presented: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  challenge_validated: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  finalizing: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  // 版本/发布状态
  stable: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  canary: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  standby: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  running: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  created: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
  completed: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
};

// 状态文案取 i18n 词表 status.<key>；无词条则回退原始值（active/enabled 等保持英文枚举原样）。
export function StatusBadge({ value, raw }: { value: string; raw?: boolean }) {
  const { t } = useTranslation('admin');
  const key = value.toLowerCase();
  const cls = color[key] ?? 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300';
  const text = t(`status.${key}`, { defaultValue: raw ? value : value });
  return (
    <span className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${cls}`}>{text}</span>
  );
}
