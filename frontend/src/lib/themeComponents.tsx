// 主题组件统一入口（wrapper 层，驻留 lib/）：消费方只从这里 import，不感知当前主题。
// 运行时按 theme store 当前主题分发到具体实现；registry eager 提供、同步渲染。
// 组件契约（props 类型）固定在此三个 wrapper：新增主题需实现同名组件，缺失自动回退 default。
import { useTheme } from '../stores/theme';
import { getThemeComponent } from './themeRegistry';
import type {
  DashboardStatCardProps,
  SideNavProps,
  PageHeaderProps,
  TopBarProps,
} from './themeTypes';

// default 主题保证实现全部契约组件，故 getThemeComponent 必然命中；非空断言仅消除 undefined。
const pick = <P,>(theme: string, name: string) =>
  getThemeComponent<P>(theme, name)!;

export function DashboardStatCard(props: DashboardStatCardProps) {
  const { theme } = useTheme();
  const Comp = pick<DashboardStatCardProps>(theme, 'DashboardStatCard');
  return <Comp {...props} />;
}

export function SideNav(props: SideNavProps) {
  const { theme } = useTheme();
  const Comp = pick<SideNavProps>(theme, 'SideNav');
  return <Comp {...props} />;
}

export function PageHeader(props: PageHeaderProps) {
  const { theme } = useTheme();
  const Comp = pick<PageHeaderProps>(theme, 'PageHeader');
  return <Comp {...props} />;
}

export function TopBar(props: TopBarProps) {
  const { theme } = useTheme();
  const Comp = pick<TopBarProps>(theme, 'TopBar');
  return <Comp {...props} />;
}
