// cyber 风格页头：主色饰条 + 渐变标题，强调式页头。
import type { PageHeaderProps } from '../../../lib/themeTypes';

export default function PageHeader({ title, right }: PageHeaderProps) {
  return (
    <div className="mb-5 flex items-center justify-between gap-3 rounded-r-2xl border-l-4 border-cyan-500 pl-4 dark:border-cyan-400">
      <h1 className="bg-gradient-to-r from-cyan-700 via-cyan-600 to-cyan-500 bg-clip-text text-2xl font-bold text-transparent dark:from-cyan-300 dark:via-cyan-200 dark:to-cyan-100">
        {title}
      </h1>
      {right && <div className="flex shrink-0 items-center gap-2">{right}</div>}
    </div>
  );
}
