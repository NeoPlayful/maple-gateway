import { useEffect } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import { useAuth } from './stores/auth';
import AdminLayout from './layouts/AdminLayout';
import LoginPage from './pages/LoginPage';
import DashboardPage from './pages/DashboardPage';
import TenantsPage from './pages/TenantsPage';
import DomainsPage from './pages/DomainsPage';
import ServicesPage from './pages/ServicesPage';
import DeploymentsPage from './pages/DeploymentsPage';
import InstancesPage from './pages/InstancesPage';
import NodesPage from './pages/NodesPage';
import TrafficPage from './pages/TrafficPage';
import CanaryPage from './pages/CanaryPage';
import LogsPage from './pages/LogsPage';
import SettingsPage from './pages/SettingsPage';
import RateLimitsPage from './pages/RateLimitsPage';
import BlueGreenPage from './pages/BlueGreenPage';
import HealthPage from './pages/HealthPage';

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

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<AdminLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="/tenants" element={<TenantsPage />} />
          <Route path="/domains" element={<DomainsPage />} />
          <Route path="/services" element={<ServicesPage />} />
          <Route path="/deployments" element={<DeploymentsPage />} />
          <Route path="/instances" element={<InstancesPage />} />
          <Route path="/nodes" element={<NodesPage />} />
          <Route path="/traffic" element={<TrafficPage />} />
          <Route path="/canary" element={<CanaryPage />} />
          <Route path="/rate-limits" element={<RateLimitsPage />} />
          <Route path="/blue-green" element={<BlueGreenPage />} />
          <Route path="/health" element={<HealthPage />} />
          <Route path="/logs" element={<LogsPage />} />
          <Route path="/settings" element={<SettingsPage />} />
        </Route>
        <Route path="*" element={<div className="p-6">404</div>} />
      </Routes>
    </BrowserRouter>
  );
}
