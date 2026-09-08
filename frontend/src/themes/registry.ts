// 主题组件注册表：静态 map theme name → 组件集合。
// Maple 固定两套主题，无需 import.meta.glob 动态扫描；结构参考 Shopirea：
// 每个主题目录 themes/<name>/components/ 提供同名展示组件，default 为默认回退主题。
import type { ComponentType } from 'react';
import type { ThemeName } from '../stores/theme';
import DefaultStat from './default/components/DashboardStatCard';
import DefaultSideNav from './default/components/SideNav';
import DefaultPageHeader from './default/components/PageHeader';
import CyberStat from './cyber/components/DashboardStatCard';
import CyberSideNav from './cyber/components/SideNav';
import CyberPageHeader from './cyber/components/PageHeader';

export interface DashboardStatCardProps {
  label: string;
  value: number;
  unit?: string;
}
export type SideNavItem = {
  to: string;
  label: string;
  end?: boolean;
  icon: ComponentType<{ className?: string }>;
};
export interface SideNavProps {
  items: SideNavItem[];
  header: React.ReactNode; // Logo 区
  footer: React.ReactNode; // 底部版本
}
export interface PageHeaderProps {
  title: string;
  right?: React.ReactNode;
}

export type ThemeComponents = {
  DashboardStatCard: ComponentType<DashboardStatCardProps>;
  SideNav: ComponentType<SideNavProps>;
  PageHeader: ComponentType<PageHeaderProps>;
};

const registry: Record<ThemeName, ThemeComponents> = {
  default: { DashboardStatCard: DefaultStat, SideNav: DefaultSideNav, PageHeader: DefaultPageHeader },
  cyber: { DashboardStatCard: CyberStat, SideNav: CyberSideNav, PageHeader: CyberPageHeader },
};

export const defaultTheme: ThemeName = 'default';

export function getThemeComponents(theme: ThemeName): ThemeComponents {
  return registry[theme] ?? registry[defaultTheme];
}
