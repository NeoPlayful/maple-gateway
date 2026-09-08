// cyber 风格统计卡：大圆角 + 主色辉光边 + hover 抬升，科技感数据块。
import type { DashboardStatCardProps } from '../../../lib/themeTypes';

export default function DashboardStatCard({ label, value, unit }: DashboardStatCardProps) {
  return (
    <div className="rounded-[1.25rem] border border-cyan-200/70 bg-white/80 p-5 shadow-[0_10px_25px_-5px_rgb(8_145_178_/_0.18),0_4px_6px_-2px_rgb(8_145_178_/_0.1)] backdrop-blur transition-transform duration-200 hover:-translate-y-0.5 hover:shadow-[0_0_0_1px_rgb(8_145_178_/_0.2),0_12px_28px_-4px_rgb(8_145_178_/_0.3)] dark:border-cyan-500/25 dark:bg-slate-800/70 dark:hover:shadow-[0_0_0_1px_rgb(34_211_238_/_0.25),0_12px_30px_-4px_rgb(34_211_238_/_0.35)]">
      <p className="text-sm font-medium text-cyan-700/80 dark:text-cyan-200/80">{label}</p>
      <p className="mt-1 bg-gradient-to-br from-slate-800 to-slate-950 bg-clip-text text-3xl font-extrabold text-transparent dark:from-white dark:to-cyan-100">
        {value}
        {unit && <span className="ml-1 bg-none text-base font-normal text-slate-400">{unit}</span>}
      </p>
      <span className="mt-3 block h-1 w-8 rounded-full bg-gradient-to-r from-cyan-500 to-cyan-300 dark:from-cyan-400 dark:to-cyan-200" />
    </div>
  );
}
