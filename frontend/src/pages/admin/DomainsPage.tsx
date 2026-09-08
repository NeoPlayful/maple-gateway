import CrudPage, { PageDef } from '../../components/CrudPage';

const def: PageDef = {
  title: 'domains.title',
  path: '/api/admin/domains',
  columns: [
    { key: 'hostname', label: 'fields.hostname' },
    { key: 'tenant_id', label: 'fields.tenant', render: (r) => r.tenant_id?.slice(0, 8) ?? '-' },
    { key: 'status', label: 'fields.status', badge: true },
    { key: 'created_at', label: 'fields.createdAt', render: (r) => new Date(r.created_at).toLocaleString() },
  ],
  createFields: [
    {
      key: 'tenant_id',
      label: 'fields.parentTenant',
      required: true,
      loadOptions: { path: '/api/admin/tenants', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'hostname', label: 'fields.hostname', required: true, placeholder: 'domains.hostnamePh' },
  ],
  statusActions: ['enable', 'disable'],
};

export default function DomainsPage() {
  return <CrudPage def={def} />;
}
