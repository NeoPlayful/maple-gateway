import CrudPage, { PageDef } from '../../components/CrudPage';

const def: PageDef = {
  title: 'services.title',
  path: '/api/admin/services',
  columns: [
    { key: 'name', label: 'fields.name' },
    { key: 'tenant_id', label: 'fields.tenant', render: (r) => r.tenant_id?.slice(0, 8) ?? '-' },
    { key: 'protocol', label: 'fields.protocol' },
    { key: 'status', label: 'fields.status', badge: true },
  ],
  filters: [
    { type: 'keyword', key: 'keyword', keys: ['name'], placeholder: 'services.filterPh' },
    { type: 'select', key: 'tenant_id', placeholder: 'common.filterAllTenant', loadOptions: { path: '/api/admin/tenants', valueKey: 'id', labelKey: 'name' } },
    { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
  ],
  createFields: [
    {
      key: 'tenant_id',
      label: 'fields.parentTenant',
      required: true,
      loadOptions: { path: '/api/admin/tenants', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'name', label: 'fields.name', required: true, placeholder: 'services.namePh' },
    {
      key: 'protocol',
      label: 'fields.protocol',
      type: 'select',
      options: [
        { value: 'http', label: 'http' },
        { value: 'https', label: 'https' },
      ],
    },
  ],
  statusActions: ['enable', 'disable'],
};

export default function ServicesPage() {
  return <CrudPage def={def} />;
}
