// 主题注册表——glob 动态发现，加主题零注册。
// 引擎驻留 lib/，themes/ 只放纯资产（每主题目录含 theme.css，可选 theme.json 与 components/*.tsx）。
// 参考 Shopirea：import.meta.glob 在构建期扫描 src/themes/*/，新增主题目录即自动纳入。
import type { ComponentType } from 'react';
import type { ThemeName } from '../stores/theme';

const DEFAULT_THEME: ThemeName = 'default';

// ── CSS 文件扫描（lazy：default 之外的 css 不进首屏，切到时才注入）──

/** key: "/src/themes/<name>/theme.css" → loader（按需注入 style） */
const themeCSSLoaders = import.meta.glob('/src/themes/*/theme.css');

/** 由目录结构派生可用主题名；default 恒居首位（兜底主题、默认入口），其余按字母序。 */
export function getAvailableThemes(): ThemeName[] {
  const themes = Object.keys(themeCSSLoaders)
    .map((path) => {
      const match = path.match(/\/themes\/([^/]+)\/theme\.css$/);
      return match ? match[1] : '';
    })
    .filter(Boolean)
    .sort();
  if (themes[0] === DEFAULT_THEME) return themes as ThemeName[];
  return [DEFAULT_THEME, ...themes.filter((t) => t !== DEFAULT_THEME)] as ThemeName[];
}

/** 动态注入某主题的 theme.css（Vite 以 <style> 形式注入，幂等）。 */
export function loadThemeCSS(theme: string): void {
  const loader = themeCSSLoaders[`/src/themes/${theme}/theme.css`];
  loader?.();
}

// ── 组件文件扫描（eager：主题组件本就全量打包，与旧的静态 registry 同款行为，同步可用）──

interface ComponentModule {
  default: ComponentType;
}

/** key: "<theme>/<Name>" → 组件。eager 保证 wrapper 同步拿到实现，无需 lazy/Suspense。 */
const componentMap = new Map<string, ComponentType>();

const componentModules = import.meta.glob<ComponentModule>(
  '/src/themes/*/components/*.tsx',
  { eager: true },
);
for (const [path, mod] of Object.entries(componentModules)) {
  const match = path.match(/\/themes\/([^/]+)\/components\/(.+)\.tsx$/);
  if (match) componentMap.set(`${match[1]}/${match[2]}`, mod.default);
}

/**
 * 解析某主题的展示组件（类型化：P 为该组件的 props 契约）；该主题缺省时回退 default。
 * 仅当 default 主题也不存在该组件时才返回 undefined。
 */
export function getThemeComponent<P = Record<string, never>>(
  theme: string,
  componentName: string,
): ComponentType<P> | undefined {
  return (
    (componentMap.get(`${theme}/${componentName}`) as ComponentType<P> | undefined) ??
    (componentMap.get(`${DEFAULT_THEME}/${componentName}`) as ComponentType<P> | undefined)
  );
}

// ── 主题元数据（theme.json）扫描（eager：JSON 极小，管理页同步读取）──

export interface ThemeMeta {
  label: string;
  labelKey?: string;
  description?: string;
  colors: string[];
}

const themeMetaModules = import.meta.glob<{ default: ThemeMeta }>(
  '/src/themes/*/theme.json',
  { eager: true },
);

export function getThemeMeta(theme: string): ThemeMeta | null {
  const mod = themeMetaModules[`/src/themes/${theme}/theme.json`];
  return mod?.default ?? null;
}
