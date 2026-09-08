import { create } from 'zustand';
import { getAvailableThemes, loadThemeCSS } from '../lib/themeRegistry';

// 主题名：已知内置 + 运行期任意字符串（未来主题由 themeRegistry glob 自动发现，不再硬编码枚举）。
// 若需保留"静态已知主题"的强类型，可只写 'default' | 'cyber'（放宽是为了加主题不改此文件）。
export type ThemeName = 'default' | 'cyber' | (string & {});
type Mode = 'light' | 'dark';

// 应用命名空间 key（与 maple_token / maple-lang 同族），不随主题名改变。
const THEME_KEY = 'maple-theme';
const MODE_KEY = 'maple-theme-mode';

const FALLBACK_THEME: ThemeName = 'default';
// 可用主题派生自各主题目录的 theme.css，加主题无需改这里。
const availableThemes = getAvailableThemes();

function readStoredTheme(): ThemeName {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved && availableThemes.includes(saved as ThemeName)) return saved as ThemeName;
  } catch {
    /* ignore */
  }
  return FALLBACK_THEME;
}

function readStoredMode(): Mode | null {
  try {
    const saved = localStorage.getItem(MODE_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    /* ignore */
  }
  return null;
}

function readInitialMode(): Mode {
  // 默认跟随系统，无系统偏好则亮色。
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

/** DOM 落点：data-theme 决定主题，data-mode 决定深浅。 */
function apply(theme: ThemeName, mode: Mode) {
  document.documentElement.setAttribute('data-theme', theme);
  document.documentElement.setAttribute('data-mode', mode);
}

function persist(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* ignore */
  }
}

interface ThemeState {
  theme: ThemeName;
  mode: Mode;
  setTheme: (theme: ThemeName) => void;
  setMode: (mode: Mode) => void;
  toggleMode: () => void;
}

const initialTheme = readStoredTheme();
const initialMode = readStoredMode() ?? readInitialMode();

export const useTheme = create<ThemeState>((set, get) => ({
  theme: initialTheme,
  mode: initialMode,

  setTheme: (theme) => {
    // 按需注入该主题 theme.css（default 已被 main.tsx 静态常驻，其余首切才加载；Vite 注入幂等）。
    loadThemeCSS(theme);
    apply(theme, get().mode);
    persist(THEME_KEY, theme);
    set({ theme });
  },

  setMode: (mode) => {
    apply(get().theme, mode);
    persist(MODE_KEY, mode);
    set({ mode });
  },

  toggleMode: () => {
    const { theme, mode } = get();
    const next: Mode = mode === 'light' ? 'dark' : 'light';
    apply(theme, next);
    persist(MODE_KEY, next);
    set({ mode: next });
  },
}));

// 模块加载即应用一次（浏览器内），避免首帧闪白 / 用了上次主题却停留在 default 样式。
if (typeof window !== 'undefined') {
  loadThemeCSS(initialTheme);
  apply(initialTheme, initialMode);
}
