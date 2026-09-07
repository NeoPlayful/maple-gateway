// 通用 UI 小组件：状态徽章、操作按钮。
const phaseColor: Record<string, string> = {
  created: 'bg-slate-100 text-slate-600',
  running: 'bg-emerald-100 text-emerald-700',
  paused: 'bg-amber-100 text-amber-700',
  completed: 'bg-blue-100 text-blue-700',
  rolled_back: 'bg-rose-100 text-rose-700',
  failed: 'bg-red-100 text-red-700',
};

export function StatusBadge({ value }: { value: string }) {
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${phaseColor[value] ?? 'bg-slate-100 text-slate-600'}`}>
      {value}
    </span>
  );
}

export function PhaseBadge({ phase }: { phase: string }) {
  const map: Record<string, string> = {
    running: '进行中',
    paused: '已暂停',
    completed: '已完成',
    rolled_back: '已回滚',
    created: '草稿',
    failed: '失败',
  };
  return <StatusBadge value={map[phase] ?? phase} />;
}
