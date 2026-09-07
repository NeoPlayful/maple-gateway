import { useEffect, useState } from 'react';
import { api } from '../api/client';

interface Card {
  label: string;
  value: number;
  unit?: string;
}

const endpoints: { label: string; path: string }[] = [
  { label: '租户', path: '/api/admin/tenants' },
  { label: '域名', path: '/api/admin/domains' },
  { label: '服务', path: '/api/admin/services' },
  { label: '实例', path: '/api/admin/instances' },
  { label: '节点', path: '/api/admin/nodes' },
  { label: '部署', path: '/api/admin/deployments' },
  { label: 'Canary 发布', path: '/api/admin/canary' },
];

export default function DashboardPage() {
  const [cards, setCards] = useState<Card[]>([]);
  const [err, setErr] = useState('');

  useEffect(() => {
    (async () => {
      const out: Card[] = [];
      for (const ep of endpoints) {
        try {
          const resp = await fetch(ep.path, {
            headers: { Authorization: `Bearer ${localStorage.getItem('maple_token') ?? ''}` },
          });
          const j = await resp.json();
          out.push({ label: ep.label, value: Number(j?.meta?.total ?? (Array.isArray(j?.data) ? j.data.length : 0)) });
        } catch {
          out.push({ label: ep.label, value: 0 });
        }
      }
      setCards(out);
    })().catch(() => setErr('总览数据加载失败'));
  }, []);

  return (
    <div>
      <h1 className="mb-6 text-xl font-semibold text-slate-800">Dashboard</h1>
      {err && <p className="mb-4 text-sm text-red-600">{err}</p>}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        {cards.map((c) => (
          <div key={c.label} className="rounded-xl bg-white p-5 shadow-sm">
            <p className="text-sm text-slate-500">{c.label}</p>
            <p className="mt-1 text-3xl font-bold text-slate-800">
              {c.value}
              {c.unit && <span className="ml-1 text-base font-normal text-slate-400">{c.unit}</span>}
            </p>
          </div>
        ))}
      </div>
      <div className="mt-6 rounded-xl bg-white p-6 text-sm text-slate-500 shadow-sm">
        Phase 2 发布控制与可视化管理已就绪：Canary / Blue-Green / Rate Limit / Metrics / Logs /
        Settings。资源页逐步填充中。
      </div>
    </div>
  );
}
