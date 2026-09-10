import CrudPage, { PageDef } from '../../components/CrudPage';
import { IdCell } from '../../components/admin/IdCell';
import { DetailGrid } from '../../components/admin/DetailGrid';

const fmtTime = (v?: string) => (v ? new Date(v).toLocaleString() : '-');

const def: PageDef = {
  title: 'tenants.title',
  path: '/api/admin/tenants',
  columns: [
    { key: 'id', label: 'fields.id', render: (r) => <IdCell id={r.id} /> },
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
  editFields: [
    { key: 'name', label: 'fields.name', required: true },
    {
      key: 'description',
      label: 'fields.description',
      type: 'textarea',
      clearOnEmpty: true, // 允许清空描述（提交 description_clear=true）
    },
  ],
  statusActions: ['enable', 'disable'],
  renderDetail: (r) => (
    <DetailGrid
      items={[
        { label: 'fields.id', value: r.id },
        { label: 'fields.slug', value: r.slug },
        { label: 'fields.status', value: r.status },
        { label: 'fields.description', value: r.description },
        { label: 'fields.createdAt', value: fmtTime(r.created_at) },
        { label: 'fields.updatedAt', value: fmtTime(r.updated_at) },
      ]}
    />
  ),
};

export default function TenantsPage() {
  return <CrudPage def={def} />;
}
