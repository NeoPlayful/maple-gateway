import { useEffect } from 'react';
import { Outlet, useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from '../stores/auth';
import TopBar from '../components/TopBar';
import { nav } from '../nav';
import { version } from '../../package.json';
import { SideNav } from '../themes';

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
      <SideNav
        items={nav}
        header={
          <>
            <span className="text-lg font-bold text-th-brand-text">🍁 Maple</span>
            <span className="text-xs text-slate-400">Gateway</span>
          </>
        }
        footer={
          <p className="text-xs text-slate-400 dark:text-slate-500">
            {t('common.version')} v{version}
          </p>
        }
      />

      <div className="flex h-full min-w-0 flex-1 flex-col">
        <TopBar />
        <main className="flex-1 overflow-y-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
