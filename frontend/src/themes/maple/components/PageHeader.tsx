// maple 扁平页头：标题左对齐 + 下方 1px 实边分隔，无投影。
import type { PageHeaderProps } from '../../../lib/themeTypes';

export default function PageHeader({ title, right }: PageHeaderProps) {
  return (
    <div className="mb-4 flex items-center justify-between gap-3 border-b border-slate-200 pb-3 dark:border-slate-800">
      <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{title}</h1>
      {right && <div className="flex shrink-0 items-center gap-2">{right}</div>}
    </div>
  );
}
