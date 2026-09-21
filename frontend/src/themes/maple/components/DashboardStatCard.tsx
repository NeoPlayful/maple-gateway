// maple 扁平统计卡：直角、无阴影，1px 实边；顶部 2px 强调条标识，数字实色。
import type { DashboardStatCardProps } from '../../../lib/themeTypes';

export default function DashboardStatCard({ label, value, unit }: DashboardStatCardProps) {
  return (
    <div className="border border-slate-200 bg-white p-5 dark:border-slate-700 dark:bg-slate-800">
      <span className="mb-3 block h-0.5 w-8 bg-th-accent" />
      <p className="text-sm text-slate-500 dark:text-slate-400">{label}</p>
      <p className="mt-1 text-3xl font-bold text-slate-800 dark:text-slate-100">
        {value}
        {unit && <span className="ml-1 text-base font-normal text-slate-400 dark:text-slate-500">{unit}</span>}
      </p>
    </div>
  );
}
