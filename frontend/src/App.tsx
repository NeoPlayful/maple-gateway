import { useEffect } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { useAuth } from './stores/auth';
import AdminLayout from './layouts/AdminLayout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import CanaryPage from './pages/CanaryPage';
import SettingsPage from './pages/SettingsPage';
import PlaceholderPage from './pages/PlaceholderPage';

export default function App() {
  const restore = useAuth((s) => s.restore);
  const initialized = useAuth((s) => s.initialized);

  useEffect(() => {
    restore();
  }, [restore]);

  if (!initialized) {
    return (
      <div className="flex h-screen items-center justify-center text-slate-400">
        加载中…
      </div>
    );
  }

  const resourcePages = ['tenants', 'domains', 'services', 'deployments', 'instances', 'nodes', 'traffic', 'logs'];
  const titles: Record<string, string> = {
    tenants: '租户管理',
    domains: '域名管理',
    services: '服务管理',
    deployments: '部署与版本',
    instances: '实例管理',
    nodes: '节点管理',
    traffic: '流量策略',
    logs: '日志查询',
  };

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<AdminLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="/canary" element={<CanaryPage />} />
          <Route path="/settings" element={<SettingsPage />} />
          {resourcePages.map((p) => (
            <Route key={p} path={`/${p}`} element={<PlaceholderPage title={titles[p]} />} />
          ))}
        </Route>
        <Route path="*" element={<div className="p-6">404</div>} />
      </Routes>
    </BrowserRouter>
  );
}
