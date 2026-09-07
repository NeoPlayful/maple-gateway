import CrudPage, { PageDef } from '../components/CrudPage';

const def: PageDef = {
  title: '租户管理',
  path: '/api/admin/tenants',
  columns: [
    { key: 'name', label: '名称' },
    { key: 'slug', label: '标识' },
    { key: 'description', label: '描述' },
    { key: 'status', label: '状态', badge: true },
  ],
  createFields: [
    { key: 'name', label: '名称', required: true },
    { key: 'slug', label: '标识 (slug)', required: true, placeholder: '如 tenant_1001' },
    { key: 'description', label: '描述', type: 'textarea' },
  ],
  statusActions: ['enable', 'disable'],
};

export default function TenantsPage() {
  return <CrudPage def={def} />;
}
