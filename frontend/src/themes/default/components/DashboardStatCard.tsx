// default 风格统计卡：紧凑扁平，细边 + 浅投影，数字+标签。
import type { DashboardStatCardProps } from '../../../lib/themeTypes';

export default function DashboardStatCard({ label, value, unit }: DashboardStatCardProps) {
  return (
    <div className="rounded-xl border border-slate-200 bg-white p-5 shadow-[0_1px_2px_0_rgb(15_23_42_/_0.06)] dark:border-slate-700 dark:bg-slate-800">
      <p className="text-sm text-slate-500 dark:text-slate-400">{label}</p>
      <p className="mt-1 text-3xl font-bold text-slate-800 dark:text-slate-100">
        {value}
        {unit && <span className="ml-1 text-base font-normal text-slate-400">{unit}</span>}
      </p>
    </div>
  );
}
