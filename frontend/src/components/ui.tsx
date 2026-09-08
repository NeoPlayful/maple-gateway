// 通用 UI 小组件：状态徽章、操作按钮。
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

export function PhaseBadge({ phase }: { phase: string }) {
  return <StatusBadge value={phase} />;
}

export function ActionBtn({
  onClick,
  danger,
  children,
  disabled,
}: {
  onClick: () => void;
  danger?: boolean;
  children: React.ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`rounded px-2 py-1 text-xs disabled:opacity-40 ${
        danger
          ? 'bg-rose-600 text-white hover:bg-rose-700'
          : 'bg-slate-200 text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600'
      }`}
    >
      {children}
    </button>
  );
}

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{label}</label>
      {children}
    </div>
  );
}

export const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';
