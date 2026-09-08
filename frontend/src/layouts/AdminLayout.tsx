import { useEffect } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../stores/auth';
import TopBar from '../components/TopBar';
import { nav } from '../nav';
import { version } from '../../package.json';

export default function AdminLayout() {
  const { t } = useTranslation('admin');
  const { admin } = useAuth();
  const navigate = useNavigate();

  useEffect(() => {
    if (!admin) navigate('/login', { replace: true });
  }, [admin, navigate]);

  if (!admin) return null;

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-slate-100 dark:bg-slate-950">
      <aside className="flex h-full w-56 shrink-0 flex-col border-r border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
        {/* Logo */}
        <div className="flex h-14 shrink-0 items-center gap-2 border-b border-slate-200 px-4 dark:border-slate-800">
          <span className="text-lg font-bold text-emerald-600 dark:text-emerald-400">🍁 Maple</span>
          <span className="text-xs text-slate-400">Gateway</span>
        </div>

        <nav className="flex-1 space-y-0.5 overflow-y-auto py-2 px-2">
          {nav.map((n) => {
            const Icon = n.icon;
            return (
              <NavLink
                key={n.to}
                to={n.to}
                end={n.end}
                className={({ isActive }) =>
                  `flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors ${
                    isActive
                      ? 'bg-emerald-50 font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
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

        {/* 底部版本号 */}
        <div className="shrink-0 border-t border-slate-200 px-4 py-3 dark:border-slate-800">
          <p className="text-xs text-slate-400 dark:text-slate-500">
            {t('common.version')} v{version}
          </p>
        </div>
      </aside>

      <div className="flex h-full min-w-0 flex-1 flex-col">
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
