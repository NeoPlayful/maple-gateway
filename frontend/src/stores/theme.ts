import { create } from 'zustand';

type Mode = 'light' | 'dark';

const LS_KEY = 'maple-theme';

function readInitial(): Mode {
  try {
    const saved = localStorage.getItem(LS_KEY);
    if (saved === 'light' || saved === 'dark') return saved;
  } catch {
    /* ignore */
  }
  // 默认跟随系统，无系统偏好则亮色。
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyMode(mode: Mode) {
  document.documentElement.classList.toggle('dark', mode === 'dark');
}

interface ThemeState {
  mode: Mode;
  toggle: () => void;
  setMode: (mode: Mode) => void;
}

export const useTheme = create<ThemeState>((set) => ({
  mode: readInitial(),
  toggle: () =>
    set((s) => {
      const next = s.mode === 'light' ? 'dark' : 'light';
      applyMode(next);
      try {
        localStorage.setItem(LS_KEY, next);
      } catch {
        /* ignore */
      }
      return { mode: next };
    }),
  setMode: (mode) => {
    applyMode(mode);
    try {
      localStorage.setItem(LS_KEY, mode);
    } catch {
      /* ignore */
    }
    set({ mode });
  },
}));

// 模块加载即应用一次（在浏览器中）。
if (typeof window !== 'undefined') {
  applyMode(readInitial());
}
