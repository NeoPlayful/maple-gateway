// default 风格页头：朴素一行标题 + 右侧操作区。
import type { PageHeaderProps } from '../../registry';

export default function PageHeader({ title, right }: PageHeaderProps) {
  return (
    <div className="mb-4 flex items-center justify-between gap-3">
      <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{title}</h1>
      {right && <div className="flex shrink-0 items-center gap-2">{right}</div>}
    </div>
  );
}
