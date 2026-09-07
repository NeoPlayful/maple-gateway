// 通用 UI 小组件：状态徽章、操作按钮。

const color: Record<string, string> = {
  // 资源状态
  active: 'bg-emerald-100 text-emerald-700',
  enabled: 'bg-emerald-100 text-emerald-700',
  online: 'bg-emerald-100 text-emerald-700',
  healthy: 'bg-emerald-100 text-emerald-700',
  // 中间态
  pending: 'bg-amber-100 text-amber-700',
  draining: 'bg-amber-100 text-amber-700',
  recovering: 'bg-amber-100 text-amber-700',
  paused: 'bg-amber-100 text-amber-700',
  // 异常
  suspended: 'bg-slate-200 text-slate-600',
  disabled: 'bg-slate-200 text-slate-600',
  offline: 'bg-slate-200 text-slate-600',
  maintenance: 'bg-slate-200 text-slate-600',
  unknown: 'bg-slate-200 text-slate-600',
  unhealthy: 'bg-rose-100 text-rose-700',
  failed: 'bg-rose-100 text-rose-700',
  rolled_back: 'bg-rose-100 text-rose-700',
  // 版本/发布状态
  stable: 'bg-emerald-100 text-emerald-700',
  canary: 'bg-amber-100 text-amber-700',
  standby: 'bg-sky-100 text-sky-700',
  running: 'bg-emerald-100 text-emerald-700',
  created: 'bg-slate-100 text-slate-600',
  completed: 'bg-blue-100 text-blue-700',
};

const zh: Record<string, string> = {
  active: 'active',
  enabled: 'enabled',
  disabled: 'disabled',
  online: 'online',
  offline: 'offline',
  maintenance: '维护中',
  healthy: '健康',
  unhealthy: '异常',
  unknown: '未知',
  recovering: '恢复中',
  draining: '排空中',
  suspended: '已暂停',
  pending: '待验证',
  stable: 'stable',
  canary: 'canary',
  standby: 'standby',
  created: '草稿',
  running: '进行中',
  paused: '已暂停',
  completed: '已完成',
  rolled_back: '已回滚',
  failed: '失败',
};

export function StatusBadge({ value, raw }: { value: string; raw?: boolean }) {
  const key = value.toLowerCase();
  const cls = color[key] ?? 'bg-slate-100 text-slate-600';
  const text = raw ? value : (zh[key] ?? value);
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
          : 'bg-slate-200 text-slate-700 hover:bg-slate-300'
      }`}
    >
      {children}
    </button>
  );
}

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs text-slate-500">{label}</label>
      {children}
    </div>
  );
}

export const inputCls =
  'w-full rounded border px-2 py-1.5 text-sm focus:border-emerald-500 focus:outline-none';
