import { useEffect, useState } from 'react';
import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../stores/auth';

const nav = [
  { to: '/admin', label: 'Dashboard', end: true },
  { to: '/admin/tenants', label: '租户' },
  { to: '/admin/domains', label: '域名' },
  { to: '/admin/services', label: '服务' },
  { to: '/admin/deployments', label: '发布' },
  { to: '/admin/instances', label: '实例' },
  { to: '/admin/nodes', label: '节点' },
  { to: '/admin/traffic', label: '流量策略' },
  { to: '/admin/canary', label: 'Canary 发布' },
  { to: '/admin/blue-green', label: '蓝绿发布' },
  { to: '/admin/rate-limits', label: '限流' },
  { to: '/admin/health', label: '健康' },
  { to: '/admin/logs', label: '日志' },
  { to: '/admin/settings', label: '设置' },
];

export default function AdminLayout() {
  const { admin, logout } = useAuth();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => {
    if (!admin) navigate('/login', { replace: true });
  }, [admin, navigate]);

  if (!admin) return null;

  return (
    // 固定侧边栏：独立 h-screen 根布局，不用 min-h-screen 嵌套，保证 overflow 正确。
    <div className="flex h-screen w-screen overflow-hidden bg-slate-100">
      <aside
        className={`flex h-full shrink-0 flex-col bg-slate-900 text-slate-200 transition-all ${
          collapsed ? 'w-14' : 'w-52'
        }`}
      >
        <div className="flex h-14 items-center gap-2 border-b border-slate-700 px-3">
          <span className="text-lg font-bold text-emerald-400">🍁 Maple</span>
          {!collapsed && <span className="text-xs text-slate-400">Gateway</span>}
        </div>
        <nav className="flex-1 overflow-y-auto py-2">
          {nav.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.end}
              className={({ isActive }) =>
                `block px-4 py-2 text-sm transition-colors ${
                  isActive ? 'bg-slate-700 font-medium text-white' : 'hover:bg-slate-800'
                }`
              }
            >
              {collapsed ? n.label.slice(0, 1) : n.label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-slate-700 p-3">
          <button
            onClick={() => {
              logout();
              navigate('/login');
            }}
            className="w-full rounded bg-slate-700 py-1.5 text-xs hover:bg-slate-600"
          >
            退出登录
          </button>
        </div>
      </aside>
      <main className="h-full flex-1 overflow-y-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}
