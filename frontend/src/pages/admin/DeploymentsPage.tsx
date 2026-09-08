import CrudPage, { PageDef } from '../../components/CrudPage';

const def: PageDef = {
  title: 'deployments.title',
  path: '/api/admin/deployments',
  columns: [
    { key: 'name', label: 'fields.deploymentName' },
    { key: 'service_id', label: 'fields.service', render: (r) => r.service_id?.slice(0, 8) ?? '-' },
    { key: 'strategy', label: 'fields.strategy' },
    { key: 'status', label: 'fields.status', badge: true },
  ],
  createFields: [
    {
      key: 'service_id',
      label: 'fields.parentService',
      required: true,
      loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'name', label: 'fields.deploymentName', required: true, placeholder: 'deployments.namePh' },
    {
      key: 'strategy',
      label: 'fields.deployStrategy',
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
