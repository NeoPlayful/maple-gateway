// 主题 barrel：保持消费方 import 路径稳定（页面只从这里拿组件，不感知主题内部）。
// 实现驻留 src/lib/（themeComponents/themeRegistry/themeTypes），themes/ 仅放各主题资产。
export { DashboardStatCard, SideNav, PageHeader } from '../lib/themeComponents';
