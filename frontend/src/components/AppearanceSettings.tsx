// 外观设置卡片：主题 + 深浅，即时生效，存 localStorage。
// 主题卡片由 themeRegistry 扫描各主题目录的 theme.json 数据驱动，加主题零改动。
import { useTranslation } from 'react-i18next';
import { useTheme } from '../stores/theme';
import { getAvailableThemes, getThemeMeta } from '../lib/themeRegistry';

export default function AppearanceSettings() {
  const { t } = useTranslation('admin');
  const { theme, mode, setTheme, setMode } = useTheme();
  const themeNames = getAvailableThemes();

  return (
    <div>
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">{t('settings.appearance')}</h2>
      </div>

      {/* 主题卡片：每主题一行（labelKey 优先，缺省用 label），色板取 theme.json colors */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        {themeNames.map((name) => {
          const meta = getThemeMeta(name) ?? { label: name, colors: ['#cccccc', '#999999', '#666666'] };
          const isActive = theme === name;
          return (
            <button
              key={name}
              onClick={() => setTheme(name)}
              className={`overflow-hidden rounded-lg border-2 text-left transition-all ${
                isActive
                  ? 'border-th-accent shadow-sm'
                  : 'border-slate-200 hover:border-slate-300 dark:border-slate-700 dark:hover:border-slate-600'
              }`}
            >
              {/* 色板示意条 */}
              <span className="flex h-8">
                {meta.colors.map((c, i) => (
                  <span key={i} className="flex-1" style={{ backgroundColor: c }} />
                ))}
              </span>
              <span className="flex items-center justify-between bg-white px-3 py-2 dark:bg-slate-800">
                <span className="text-sm font-medium text-slate-700 dark:text-slate-200">
                  {meta.labelKey ? t(meta.labelKey) : meta.label}
                </span>
                {isActive && (
                  <span className="rounded-full bg-th-accent-soft-bg px-2 py-0.5 text-xs font-medium text-th-accent-soft-text">
                    {t('settings.currentTheme')}
                  </span>
                )}
              </span>
            </button>
          );
        })}
      </div>

      {/* 深浅模式 */}
      <div className="mt-3 flex items-center gap-2">
        <span className="text-xs text-slate-500 dark:text-slate-400">{t('settings.mode')}</span>
        {(['light', 'dark'] as const).map((m) => (
          <button
            key={m}
            onClick={() => setMode(m)}
            className={`rounded-md border px-3 py-1 text-sm transition-colors ${
              mode === m
                ? 'border-th-accent bg-th-accent-soft-bg text-th-accent-soft-text font-medium'
                : 'border-slate-200 text-slate-600 hover:bg-slate-50 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800'
            }`}
          >
            {m === 'light' ? t('settings.lightMode') : t('settings.darkMode')}
          </button>
        ))}
      </div>
    </div>
  );
}
