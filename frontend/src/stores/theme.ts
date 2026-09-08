import { create } from 'zustand';

export type ThemeName = 'default' | 'cyber';
type Mode = 'light' | 'dark';

// 应用命名空间 key（与 maple_token / maple-lang 同族），不随主题名改变。
const THEME_KEY = 'maple-theme';
const MODE_KEY = 'maple-theme-mode';

const THEMES: ThemeName[] = ['default', 'cyber'];

function readStored<T extends string>(key: string, valid: readonly T[]): T | null {
  try {
    const saved = localStorage.getItem(key) as T | null;
    if (saved && valid.includes(saved)) return saved;
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

const initialTheme = readStored<ThemeName>(THEME_KEY, THEMES) ?? 'default';
const initialMode = readStored<Mode>(MODE_KEY, ['light', 'dark']) ?? readInitialMode();

export const useTheme = create<ThemeState>((set, get) => ({
  theme: initialTheme,
  mode: initialMode,

  setTheme: (theme) => {
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

// 模块加载即应用一次（在浏览器中），避免首帧闪白。
if (typeof window !== 'undefined') {
  apply(initialTheme, initialMode);
}
