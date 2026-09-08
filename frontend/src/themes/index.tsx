// 主题组件统一入口：消费方只 import 这里的 wrapper，不感知当前主题。
// 运行时按 theme store 的当前主题分发到 default/cyber 具体实现。
import { useTheme } from '../stores/theme';
import { getThemeComponents, type DashboardStatCardProps, type SideNavProps, type PageHeaderProps } from './registry';

export function DashboardStatCard(props: DashboardStatCardProps) {
  const { theme } = useTheme();
  const Comp = getThemeComponents(theme).DashboardStatCard;
  return <Comp {...props} />;
}

export function SideNav(props: SideNavProps) {
  const { theme } = useTheme();
  const Comp = getThemeComponents(theme).SideNav;
  return <Comp {...props} />;
}

export function PageHeader(props: PageHeaderProps) {
  const { theme } = useTheme();
  const Comp = getThemeComponents(theme).PageHeader;
  return <Comp {...props} />;
}
