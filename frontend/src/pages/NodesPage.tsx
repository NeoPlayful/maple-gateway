import CrudPage, { PageDef } from '../components/CrudPage';

const def: PageDef = {
  title: '节点管理',
  path: '/api/admin/nodes',
  columns: [
    { key: 'name', label: '节点名' },
    { key: 'host', label: '地址' },
    { key: 'region', label: '区域', render: (r) => r.region ?? '-' },
    { key: 'status', label: '状态', badge: true },
    {
      key: 'last_seen_at',
      label: '最近心跳',
      render: (r) => (r.last_seen_at ? new Date(r.last_seen_at).toLocaleString() : '-'),
    },
  ],
  createFields: [
    { key: 'name', label: '节点名', required: true, placeholder: '如 node-01' },
    { key: 'host', label: '地址', required: true, placeholder: '如 192.168.1.10' },
    { key: 'region', label: '区域' },
  ],
  statusActions: ['enable', 'disable'],
  extraActions: [{ label: '维护', act: 'maintenance' }],
};

export default function NodesPage() {
  return <CrudPage def={def} />;
}
