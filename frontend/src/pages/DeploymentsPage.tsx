import CrudPage, { PageDef } from '../components/CrudPage';

const def: PageDef = {
  title: '部署与版本',
  path: '/api/admin/deployments',
  columns: [
    { key: 'name', label: '部署名' },
    { key: 'service_id', label: '服务', render: (r) => r.service_id?.slice(0, 8) ?? '-' },
    { key: 'strategy', label: '策略' },
    { key: 'status', label: '状态', badge: true },
  ],
  createFields: [
    {
      key: 'service_id',
      label: '所属服务',
      required: true,
      loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'name', label: '部署名', required: true, placeholder: '如 prod' },
    {
      key: 'strategy',
      label: '发布策略',
      type: 'select',
      options: [
        { value: 'rolling', label: 'rolling' },
        { value: 'recreate', label: 'recreate' },
        { value: 'blue_green', label: 'blue_green' },
      ],
    },
  ],
};

export default function DeploymentsPage() {
  return <CrudPage def={def} />;
}
