import { create } from 'zustand';
import { getAvailableThemes, loadThemeCSS } from '../lib/themeRegistry';
import { api } from '../lib/client';
import { useAuth } from './auth';

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

// 保存状态：idle=初始/无操作；saving=写库中；saved=刚保存成功；error=保存失败。
export type SaveStatus = 'idle' | 'saving' | 'saved' | 'error';

interface ThemeState {
  theme: ThemeName;
  mode: Mode;
  /** 最近一次已入库（或从库读入）的值；未保存改动判定基于它。 */
  savedTheme: ThemeName;
  savedMode: Mode;
  saveStatus: SaveStatus;
  setTheme: (theme: ThemeName) => void;
  setMode: (mode: Mode) => void;
  toggleMode: () => void;
  /** 有未保存改动时返回 true（当前选择 ≠ 最近入库值）。 */
  isDirty: () => boolean;
  /** 登录后从后端拉取站点级外观设置（DB 有值则覆盖本地，跨浏览器一致）。 */
  readFromServer: () => Promise<void>;
  /** 把当前 theme+mode 一次写入后端（保存按钮唯一入口）。 */
  saveToServer: () => Promise<void>;
}

// 后端 settings 单条结构：{ value, version, updated_at }。
interface SettingItem {
  value: unknown;
  version: number;
  updated_at: string;
}
type AppearanceResp = { appearance?: Record<string, SettingItem> };

/** 站点级持久化是否已启用：需登录态（admin 非空）。未登录跳过，避免登录页误写库。 */
function isAuthed(): boolean {
  return useAuth.getState().admin !== null;
}

const initialTheme = readStoredTheme();
const initialMode = readStoredMode() ?? readInitialMode();

export const useTheme = create<ThemeState>((set, get) => ({
  theme: initialTheme,
  mode: initialMode,
  savedTheme: initialTheme,
  savedMode: initialMode,
  saveStatus: 'idle',

  // 点选/切换只改本地预览（内存 + localStorage）；入库唯一入口是 saveToServer()。
  setTheme: (theme) => {
    loadThemeCSS(theme);
    apply(theme, get().mode);
    persist(THEME_KEY, theme);
    // 任何本地改动都清掉"已保存"状态，交由 isDirty 重新判定。
    set({ theme, saveStatus: 'idle' });
  },

  setMode: (mode) => {
    apply(get().theme, mode);
    persist(MODE_KEY, mode);
    set({ mode, saveStatus: 'idle' });
  },

  toggleMode: () => {
    const { theme, mode } = get();
    const next: Mode = mode === 'light' ? 'dark' : 'light';
    apply(theme, next);
    persist(MODE_KEY, next);
    set({ mode: next, saveStatus: 'idle' });
  },

  isDirty: () => {
    const { theme, mode, savedTheme, savedMode } = get();
    return theme !== savedTheme || mode !== savedMode;
  },

  readFromServer: async () => {
    if (!isAuthed()) return;
    try {
      const res = await api.get<AppearanceResp>('/api/admin/settings?section=appearance');
      const map = res?.appearance;
      if (!map) return;
      const t = map.theme?.value;
      const m = map.theme_mode?.value;
      const { theme, mode } = get();
      // DB 有合法已保存值时覆盖本地（含未保存的本地预览——未保存者不持久化）。
      if (typeof t === 'string' && t !== theme && availableThemes.includes(t as ThemeName)) {
        loadThemeCSS(t);
        apply(t as ThemeName, mode);
        persist(THEME_KEY, t);
      }
      if (m === 'light' || m === 'dark') {
        apply(get().theme, m);
        persist(MODE_KEY, m);
      }
      const finalTheme = (typeof t === 'string' && availableThemes.includes(t as ThemeName) ? t : theme) as ThemeName;
      const finalMode: Mode = (m === 'light' || m === 'dark' ? m : get().mode) as Mode;
      set({ theme: finalTheme, mode: finalMode, savedTheme: finalTheme, savedMode: finalMode, saveStatus: 'idle' });
    } catch {
      /* 后端不可用或未登录时保持本地主题 */
    }
  },

  saveToServer: async () => {
    if (!isAuthed()) return;
    const { theme, mode } = get();
    set({ saveStatus: 'saving' });
    try {
      await api.patch('/api/admin/settings/appearance', { theme, theme_mode: mode });
      set({ savedTheme: theme, savedMode: mode, saveStatus: 'saved' });
    } catch {
      set({ saveStatus: 'error' });
    }
  },
}));

// 模块加载即应用一次（浏览器内），避免首帧闪白 / 用了上次主题却停留在 default 样式。
if (typeof window !== 'undefined') {
  loadThemeCSS(initialTheme);
  apply(initialTheme, initialMode);
}
