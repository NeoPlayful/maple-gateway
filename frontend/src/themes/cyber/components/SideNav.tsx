// cyber 风格侧边导航：胶囊激活项、图标置于色块、hover 发光。
import { NavLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { SideNavProps } from '../../registry';

export default function SideNav({ items, header, footer }: SideNavProps) {
  const { t } = useTranslation('admin');
  return (
    <aside className="flex h-full w-60 shrink-0 flex-col border-r border-cyan-900/10 bg-gradient-to-b from-cyan-50/60 via-white to-white dark:border-cyan-400/10 dark:from-slate-900 dark:via-slate-900 dark:to-slate-900">
      <div className="flex h-14 shrink-0 items-center gap-2 px-4">{header}</div>
      <nav className="flex-1 space-y-1 overflow-y-auto px-2.5 py-2">
        {items.map((n) => {
          const Icon = n.icon;
          return (
            <NavLink key={n.to} to={n.to} end={n.end}>
              {({ isActive }) => (
                <span
                  className={`group mt-0.5 flex items-center gap-3 rounded-full px-2.5 py-2 text-sm transition-all duration-150 ${
                    isActive
                      ? 'bg-th-accent font-medium text-white shadow-[0_0_14px_rgb(6_182_212_/_0.45)] dark:shadow-[0_0_16px_rgb(34_211_238_/_0.4)]'
                      : 'text-slate-600 hover:bg-cyan-100/60 hover:text-cyan-800 dark:text-slate-300 dark:hover:bg-cyan-400/10 dark:hover:text-cyan-200'
                  }`}
                >
                  <span
                    className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition-colors ${
                      isActive
                        ? 'bg-white/20 text-white'
                        : 'bg-cyan-100/70 text-cyan-700 group-hover:bg-cyan-200/70 dark:bg-cyan-400/10 dark:text-cyan-300 dark:group-hover:bg-cyan-400/20'
                    }`}
                  >
                    <Icon className="h-4 w-4" />
                  </span>
                  <span className="truncate">{t(n.label)}</span>
                </span>
              )}
            </NavLink>
          );
        })}
      </nav>
      <div className="shrink-0 border-t border-cyan-900/10 px-4 py-3 dark:border-cyan-400/10">{footer}</div>
    </aside>
  );
}
