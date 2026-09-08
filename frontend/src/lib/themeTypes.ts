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
