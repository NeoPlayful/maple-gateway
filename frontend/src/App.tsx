import { useEffect } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useAuth } from './stores/auth';
import { useTheme } from './stores/theme';
import AdminLayout from './layouts/AdminLayout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/admin/DashboardPage';
import TenantsPage from './pages/admin/TenantsPage';
import DomainsPage from './pages/admin/DomainsPage';
import CertificatesPage from './pages/admin/CertificatesPage';
import ServicesPage from './pages/admin/ServicesPage';
import DeploymentsPage from './pages/admin/DeploymentsPage';
import InstancesPage from './pages/admin/InstancesPage';
import NodesPage from './pages/admin/NodesPage';
import TrafficPage from './pages/admin/TrafficPage';
import CanaryPage from './pages/admin/CanaryPage';
import LogsPage from './pages/admin/LogsPage';
import SettingsPage from './pages/admin/SettingsPage';
import RateLimitsPage from './pages/admin/RateLimitsPage';
import BlueGreenPage from './pages/admin/BlueGreenPage';
import HealthPage from './pages/admin/HealthPage';

export default function App() {
  const { t } = useTranslation('admin');
  const restore = useAuth((s) => s.restore);
  const initialized = useAuth((s) => s.initialized);

  useEffect(() => {
    restore();
  }, [restore]);

  // 登录恢复完成后，从后端拉取站点级外观（主题/模式）做跨浏览器同步；
  // 未登录时 readFromServer 内部直接跳过。restore 内 admin 与 initialized 同批 set，故在此判断可靠。
  const readServerTheme = useTheme((s) => s.readFromServer);
  useEffect(() => {
    if (initialized) readServerTheme();
  }, [initialized, readServerTheme]);

  if (!initialized) {
    return (
      <div className="flex h-screen items-center justify-center text-slate-400 dark:text-slate-500">
        {t('app.loading', '加载中…')}
      </div>
    );
  }

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/admin" element={<AdminLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="tenants" element={<TenantsPage />} />
          <Route path="domains" element={<DomainsPage />} />
          <Route path="certificates" element={<CertificatesPage />} />
          <Route path="services" element={<ServicesPage />} />
          <Route path="deployments" element={<DeploymentsPage />} />
          <Route path="instances" element={<InstancesPage />} />
          <Route path="nodes" element={<NodesPage />} />
          <Route path="traffic" element={<TrafficPage />} />
          <Route path="canary" element={<CanaryPage />} />
          <Route path="rate-limits" element={<RateLimitsPage />} />
          <Route path="blue-green" element={<BlueGreenPage />} />
          <Route path="health" element={<HealthPage />} />
          <Route path="logs" element={<LogsPage />} />
          <Route path="settings" element={<SettingsPage />} />
        </Route>
        <Route path="*" element={<div className="p-6">404</div>} />
      </Routes>
    </BrowserRouter>
  );
}
