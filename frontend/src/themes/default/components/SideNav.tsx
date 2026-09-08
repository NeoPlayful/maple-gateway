// default 风格侧边导航：紧凑扁平、小圆角激活块。
import { NavLink } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { SideNavProps } from '../../../lib/themeTypes';

export default function SideNav({ items, header, footer }: SideNavProps) {
  const { t } = useTranslation('admin');
  return (
    <aside className="flex h-full w-56 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <div className="flex h-14 shrink-0 items-center gap-2 border-b border-slate-200 px-4 dark:border-slate-800">{header}</div>
      <nav className="flex-1 space-y-0.5 overflow-y-auto px-2 py-2">
        {items.map((n) => {
          const Icon = n.icon;
          return (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
                  isActive
                    ? 'bg-th-nav-active-bg font-medium text-th-nav-active-text'
                    : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-300 dark:hover:bg-slate-800 dark:hover:text-white'
                }`
              }
            >
              <Icon className="h-4 w-4 shrink-0" />
              {t(n.label)}
            </NavLink>
          );
        })}
      </nav>
      <div className="shrink-0 border-t border-slate-200 px-4 py-3 dark:border-slate-800">{footer}</div>
    </aside>
  );
}
