import CrudPage, { PageDef } from '../../components/CrudPage';

const def: PageDef = {
  title: 'nodes.title',
  path: '/api/admin/nodes',
  columns: [
    { key: 'name', label: 'fields.nodeName' },
    { key: 'host', label: 'fields.host' },
    { key: 'region', label: 'fields.region', render: (r) => r.region ?? '-' },
    { key: 'status', label: 'fields.status', badge: true },
    {
      key: 'last_seen_at',
      label: 'fields.lastHeartbeat',
      render: (r) => (r.last_seen_at ? new Date(r.last_seen_at).toLocaleString() : '-'),
    },
  ],
  createFields: [
    { key: 'name', label: 'fields.nodeName', required: true, placeholder: 'nodes.namePh' },
    { key: 'host', label: 'fields.host', required: true, placeholder: 'nodes.hostPh' },
    { key: 'region', label: 'fields.region' },
  ],
  statusActions: ['enable', 'disable'],
  extraActions: [{ label: 'nodes.maintenance', act: 'maintenance' }],
};

export default function NodesPage() {
  return <CrudPage def={def} />;
}
