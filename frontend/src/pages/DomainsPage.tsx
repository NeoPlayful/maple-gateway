import CrudPage, { PageDef } from '../components/CrudPage';

const def: PageDef = {
  title: '域名管理',
  path: '/api/admin/domains',
  columns: [
    { key: 'hostname', label: '域名' },
    { key: 'tenant_id', label: '租户', render: (r) => r.tenant_id?.slice(0, 8) ?? '-' },
    { key: 'status', label: '状态', badge: true },
    { key: 'created_at', label: '创建时间', render: (r) => new Date(r.created_at).toLocaleString() },
  ],
  createFields: [
    {
      key: 'tenant_id',
      label: '所属租户',
      required: true,
      loadOptions: { path: '/api/admin/tenants', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'hostname', label: '域名', required: true, placeholder: '如 shop-a.com' },
  ],
  statusActions: ['enable', 'disable'],
};

export default function DomainsPage() {
  return <CrudPage def={def} />;
}
