import { useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import {
  ArrowLeftEndOnRectangleIcon,
  ChevronDownIcon,
  Cog6ToothIcon,
  MoonIcon,
  SunIcon,
  LanguageIcon,
} from '@heroicons/react/24/outline';
import { useAuth } from '../stores/auth';
import { useTheme } from '../stores/theme';
import { changeSiteLanguage } from '../i18n';

export default function TopBar() {
  const { t, i18n } = useTranslation('admin');
  const navigate = useNavigate();
  const { admin, logout } = useAuth();
  const { mode, toggleMode } = useTheme();
  const [userOpen, setUserOpen] = useState(false);
  const [langOpen, setLangOpen] = useState(false);
  const userRef = useRef<HTMLDivElement>(null);
  const langRef = useRef<HTMLDivElement>(null);

  const lang = i18n.language?.startsWith('zh') ? 'zh-CN' : 'en-US';
  const currentLangLabel = lang === 'zh-CN' ? '中文' : 'English';

  const handleLogout = () => {
    logout();
    navigate('/login', { replace: true });
  };

  return (
    <header className="flex h-14 shrink-0 items-center border-b border-slate-200 bg-white px-4 dark:border-slate-700 dark:bg-slate-900">
      {/* 右控件组靠右：ml-auto 推到最右侧，左侧留空 */}
      <div className="ml-auto flex items-center gap-2">
        {/* 语言切换 */}
        <div className="relative" ref={langRef}>
          <button
            onClick={() => {
              setLangOpen((v) => !v);
              setUserOpen(false);
            }}
            className="flex items-center gap-1 rounded-md px-2 py-1.5 text-sm text-slate-600 transition-colors hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
            title={t('topbar.language', '语言')}
          >
            <LanguageIcon className="h-5 w-5" />
            <span className="hidden sm:inline">{currentLangLabel}</span>
          </button>
          {langOpen && (
            <div
              className="absolute right-0 top-full z-50 mt-1 w-32 rounded-lg border border-slate-200 bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-800"
              onMouseLeave={() => setLangOpen(false)}
            >
              {(['zh-CN', 'en-US'] as const).map((code) => (
                <button
                  key={code}
                  onClick={() => {
                    changeSiteLanguage(code);
                    setLangOpen(false);
                  }}
                  className={`block w-full px-3 py-1.5 text-left text-sm hover:bg-slate-100 dark:hover:bg-slate-700 ${
                    lang === code
                      ? 'font-medium text-th-accent-text'
                      : 'text-slate-700 dark:text-slate-200'
                  }`}
                >
                  {code === 'zh-CN' ? '中文' : 'English'}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* 深浅切换 */}
        <button
          onClick={toggleMode}
          className="flex items-center rounded-md p-2 text-slate-600 transition-colors hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800"
          title={mode === 'dark' ? t('topbar.switchToLight', '切换亮色') : t('topbar.switchToDark', '切换暗色')}
        >
          {mode === 'dark' ? <SunIcon className="h-5 w-5" /> : <MoonIcon className="h-5 w-5" />}
        </button>

        {/* 用户下拉 */}
        {admin && (
          <div className="relative" ref={userRef}>
            <button
              onClick={() => {
                setUserOpen((v) => !v);
                setLangOpen(false);
              }}
              className="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-slate-700 transition-colors hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-800"
            >
              <span className="flex h-7 w-7 items-center justify-center rounded-full bg-th-accent-soft-bg text-xs font-semibold text-th-accent-soft-text">
                {admin.name?.charAt(0)?.toUpperCase() ?? 'A'}
              </span>
              <span className="hidden max-w-32 truncate sm:inline">{admin.name}</span>
              <ChevronDownIcon className={`h-4 w-4 transition-transform ${userOpen ? 'rotate-180' : ''}`} />
            </button>
            {userOpen && (
              <div
                className="absolute right-0 top-full z-50 mt-1 w-52 rounded-lg border border-slate-200 bg-white py-1 shadow-lg dark:border-slate-700 dark:bg-slate-800"
                onMouseLeave={() => setUserOpen(false)}
              >
                <div className="border-b border-slate-100 px-3 py-2 text-xs text-slate-400 dark:border-slate-700 dark:text-slate-400">
                  {admin.email}
                </div>
                <button
                  onClick={() => navigate('/admin/settings', { replace: false })}
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm text-slate-700 transition-colors hover:bg-slate-100 dark:text-slate-200 dark:hover:bg-slate-700"
                >
                  <Cog6ToothIcon className="h-4 w-4" />
                  {t('topbar.settings', '设置')}
                </button>
                <hr className="my-1 border-slate-100 dark:border-slate-700" />
                <button
                  onClick={handleLogout}
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm text-rose-600 transition-colors hover:bg-rose-50 dark:text-rose-400 dark:hover:bg-rose-900/30"
                >
                  <ArrowLeftEndOnRectangleIcon className="h-4 w-4" />
                  {t('topbar.logout', '退出登录')}
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </header>
  );
}
