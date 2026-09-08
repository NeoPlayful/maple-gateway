import CrudPage, { PageDef } from '../../components/CrudPage';

const def: PageDef = {
  title: 'tenants.title',
  path: '/api/admin/tenants',
  columns: [
    { key: 'name', label: 'fields.name' },
    { key: 'slug', label: 'fields.slug' },
    { key: 'description', label: 'fields.description' },
    { key: 'status', label: 'fields.status', badge: true },
  ],
  createFields: [
    { key: 'name', label: 'fields.name', required: true },
    { key: 'slug', label: 'fields.slug', required: true, placeholder: 'tenants.slugPh' },
    { key: 'description', label: 'fields.description', type: 'textarea' },
  ],
  statusActions: ['enable', 'disable'],
};

export default function TenantsPage() {
  return <CrudPage def={def} />;
}
