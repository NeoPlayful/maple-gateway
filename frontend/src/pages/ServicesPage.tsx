import CrudPage, { PageDef } from '../components/CrudPage';

const def: PageDef = {
  title: '服务管理',
  path: '/api/admin/services',
  columns: [
    { key: 'name', label: '名称' },
    { key: 'tenant_id', label: '租户', render: (r) => r.tenant_id?.slice(0, 8) ?? '-' },
    { key: 'protocol', label: '协议' },
    { key: 'status', label: '状态', badge: true },
  ],
  createFields: [
    {
      key: 'tenant_id',
      label: '所属租户',
      required: true,
      loadOptions: { path: '/api/admin/tenants', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'name', label: '服务名', required: true, placeholder: '如 storefront' },
    {
      key: 'protocol',
      label: '协议',
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
