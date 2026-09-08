// 主题系统共享类型：与具体主题无关，供 lib/themeRegistry 与各主题组件 import type。
import type { ComponentType, ReactNode } from 'react';

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
  header: ReactNode; // Logo 区
  footer: ReactNode; // 底部版本
}

export interface PageHeaderProps {
  title: string;
  right?: ReactNode;
}

/**
 * TopBar 契约：自治组件（方案 A）。主题实现自行读取 auth/theme/i18n/路由，
 * 并自持语言、深浅、用户三组下拉的 open 状态与互斥逻辑，故不接收 props。
 * 未来若需注入未读角标等外部数据，在此扩展字段。
 */
export interface TopBarProps {}
