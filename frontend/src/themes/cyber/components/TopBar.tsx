// cyber 风格顶栏：半透明渐变玻璃底、胶囊按钮、辉光圆角下拉面板。
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
import { useAuth } from '../../../stores/auth';
import { useTheme } from '../../../stores/theme';
import { changeSiteLanguage } from '../../../i18n';
import type { TopBarProps } from '../../../lib/themeTypes';

export default function TopBar(_props: TopBarProps) {
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
    <header className="flex h-14 shrink-0 items-center border-b border-cyan-900/10 bg-gradient-to-r from-white/70 via-white/40 to-cyan-100/60 px-4 backdrop-blur dark:border-cyan-400/10 dark:from-slate-900/80 dark:via-slate-900/60 dark:to-cyan-950/60">
      {/* 右控件组靠右 */}
      <div className="ml-auto flex items-center gap-1.5">
        {/* 语言切换 */}
        <div className="relative" ref={langRef}>
          <button
            onClick={() => {
              setLangOpen((v) => !v);
              setUserOpen(false);
            }}
            className="flex items-center gap-1.5 rounded-full border border-cyan-900/10 bg-white/50 px-3 py-1.5 text-sm text-cyan-800 transition-all hover:border-cyan-500/40 hover:bg-white/80 hover:shadow-[0_0_10px_rgb(8_145_178_/_0.25)] dark:border-cyan-400/10 dark:bg-white/5 dark:text-cyan-200 dark:hover:border-cyan-400/40 dark:hover:bg-white/10 dark:hover:shadow-[0_0_12px_rgb(34_211_238_/_0.3)]"
            title={t('topbar.language', '语言')}
          >
            <LanguageIcon className="h-5 w-5" />
            <span className="hidden sm:inline">{currentLangLabel}</span>
          </button>
          {langOpen && (
            <div
              className="absolute right-0 top-full z-50 mt-2 w-32 overflow-hidden rounded-2xl border border-cyan-900/15 bg-white/95 py-1 shadow-[0_8px_24px_-6px_rgb(8_145_178_/_0.35),0_0_0_1px_rgb(8_145_178_/_0.08)] backdrop-blur dark:border-cyan-400/15 dark:bg-slate-900/90 dark:shadow-[0_8px_26px_-6px_rgb(0_0_0_/_0.7),0_0_0_1px_rgb(34_211_238_/_0.15)]"
              onMouseLeave={() => setLangOpen(false)}
            >
              {(['zh-CN', 'en-US'] as const).map((code) => (
                <button
                  key={code}
                  onClick={() => {
                    changeSiteLanguage(code);
                    setLangOpen(false);
                  }}
                  className={`block w-full px-3 py-1.5 text-left text-sm transition-colors ${
                    lang === code
                      ? 'bg-cyan-100/70 font-medium text-cyan-800 dark:bg-cyan-400/10 dark:text-cyan-300'
                      : 'text-slate-700 hover:bg-cyan-100/40 dark:text-slate-200 dark:hover:bg-cyan-400/10'
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
          className="flex items-center rounded-full border border-cyan-900/10 bg-white/50 p-2 text-cyan-800 transition-all hover:border-cyan-500/40 hover:bg-white/80 hover:shadow-[0_0_10px_rgb(8_145_178_/_0.25)] dark:border-cyan-400/10 dark:bg-white/5 dark:text-cyan-200 dark:hover:border-cyan-400/40 dark:hover:bg-white/10 dark:hover:shadow-[0_0_12px_rgb(34_211_238_/_0.3)]"
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
              className="flex items-center gap-2 rounded-full border border-cyan-900/10 bg-white/50 py-1 pl-1 pr-2.5 text-sm text-cyan-900 transition-all hover:border-cyan-500/40 hover:bg-white/80 hover:shadow-[0_0_10px_rgb(8_145_178_/_0.25)] dark:border-cyan-400/10 dark:bg-white/5 dark:text-cyan-100 dark:hover:border-cyan-400/40 dark:hover:bg-white/10 dark:hover:shadow-[0_0_12px_rgb(34_211_238_/_0.3)]"
            >
              <span className="flex h-7 w-7 items-center justify-center rounded-full bg-gradient-to-br from-cyan-500 to-blue-600 text-xs font-bold text-white shadow-[0_0_10px_rgb(6_182_212_/_0.5)]">
                {admin.name?.charAt(0)?.toUpperCase() ?? 'A'}
              </span>
              <span className="hidden max-w-32 truncate sm:inline">{admin.name}</span>
              <ChevronDownIcon className={`h-4 w-4 transition-transform ${userOpen ? 'rotate-180' : ''}`} />
            </button>
            {userOpen && (
              <div
                className="absolute right-0 top-full z-50 mt-2 w-52 overflow-hidden rounded-2xl border border-cyan-900/15 bg-white/95 py-1 shadow-[0_8px_24px_-6px_rgb(8_145_178_/_0.35),0_0_0_1px_rgb(8_145_178_/_0.08)] backdrop-blur dark:border-cyan-400/15 dark:bg-slate-900/90 dark:shadow-[0_8px_26px_-6px_rgb(0_0_0_/_0.7),0_0_0_1px_rgb(34_211_238_/_0.15)]"
                onMouseLeave={() => setUserOpen(false)}
              >
                <div className="border-b border-cyan-900/10 px-3 py-2 text-xs text-cyan-700/70 dark:border-cyan-400/10 dark:text-cyan-200/60">
                  {admin.email}
                </div>
                <button
                  onClick={() => navigate('/admin/settings', { replace: false })}
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm text-slate-700 transition-colors hover:bg-cyan-100/50 dark:text-slate-200 dark:hover:bg-cyan-400/10"
                >
                  <Cog6ToothIcon className="h-4 w-4" />
                  {t('topbar.settings', '设置')}
                </button>
                <hr className="my-1 border-cyan-900/10 dark:border-cyan-400/10" />
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
