// 后台导航定义：侧边栏（AdminLayout）等共用，避免多处维护漂移。
import type { ComponentType } from 'react';
import {
  Squares2X2Icon,
  BuildingOffice2Icon,
  UsersIcon,
  GlobeAltIcon,
  ServerStackIcon,
  RocketLaunchIcon,
  CubeIcon,
  CpuChipIcon,
  ArrowsRightLeftIcon,
  BugAntIcon,
  ArrowPathIcon,
  BoltIcon,
  HeartIcon,
  DocumentTextIcon,
  Cog6ToothIcon,
  ShieldCheckIcon,
} from '@heroicons/react/24/outline';

export interface NavItem {
  to: string;
  label: string; // i18n key
  end?: boolean;
  icon: ComponentType<{ className?: string }>;
}

export const nav: NavItem[] = [
  { to: '/admin', label: 'nav.dashboard', end: true, icon: Squares2X2Icon },
  { to: '/admin/users', label: 'nav.users', icon: UsersIcon },
  { to: '/admin/tenants', label: 'nav.tenants', icon: BuildingOffice2Icon },
  { to: '/admin/domains', label: 'nav.domains', icon: GlobeAltIcon },
  { to: '/admin/certificates', label: 'nav.certificates', icon: ShieldCheckIcon },
  { to: '/admin/services', label: 'nav.services', icon: ServerStackIcon },
  { to: '/admin/deployments', label: 'nav.deployments', icon: RocketLaunchIcon },
  { to: '/admin/instances', label: 'nav.instances', icon: CubeIcon },
  { to: '/admin/nodes', label: 'nav.nodes', icon: CpuChipIcon },
  { to: '/admin/traffic', label: 'nav.traffic', icon: ArrowsRightLeftIcon },
  { to: '/admin/canary', label: 'nav.canary', icon: BugAntIcon },
  { to: '/admin/blue-green', label: 'nav.blueGreen', icon: ArrowPathIcon },
  { to: '/admin/rate-limits', label: 'nav.rateLimits', icon: BoltIcon },
  { to: '/admin/health', label: 'nav.health', icon: HeartIcon },
  { to: '/admin/logs', label: 'nav.logs', icon: DocumentTextIcon },
  { to: '/admin/settings', label: 'nav.settings', icon: Cog6ToothIcon },
];
