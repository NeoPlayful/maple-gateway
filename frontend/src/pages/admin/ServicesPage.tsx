import CrudPage, { PageDef } from '../../components/CrudPage';
import { shortId } from '../../lib/ids';

const def: PageDef = {
  title: 'services.title',
  path: '/api/admin/services',
  columns: [
    { key: 'name', label: 'fields.name' },
    // 项目列展示项目标识（项目名）；未归属回退为「-」。
    {
      key: 'project_id',
      label: 'fields.project',
      render: (r, parents) => {
        if (!r.project_id) return '-';
        const proj = (parents['/api/admin/projects'] ?? []).find((p) => p.id === r.project_id);
        return proj?.name ?? shortId(r.project_id);
      },
    },
    { key: 'protocol', label: 'fields.protocol' },
    { key: 'status', label: 'fields.status', badge: true },
  ],
  filters: [
    { type: 'keyword', key: 'keyword', keys: ['name'], placeholder: 'services.filterPh' },
    { type: 'select', key: 'project_id', placeholder: 'services.filterAllProject', loadOptions: { path: '/api/admin/projects', valueKey: 'id', labelKey: 'name' } },
    { type: 'select', key: 'status', placeholder: 'common.filterAllStatus', labelPrefix: 'status' },
  ],
  createFields: [
    {
      key: 'project_id',
      label: 'fields.parentProject',
      required: true,
      loadOptions: { path: '/api/admin/projects', valueKey: 'id', labelKey: 'name' },
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
  // 编辑含项目归属：可把存量未归属服务挂到项目，或修正归属（改归属连带继承其租户）。
  // 留空=不改归属（部分更新），便于只改名称/协议而无需重新选项目。
  editFields: [
    {
      key: 'project_id',
      label: 'fields.parentProject',
      loadOptions: { path: '/api/admin/projects', valueKey: 'id', labelKey: 'name' },
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
    {
      key: 'status',
      label: 'fields.status',
      type: 'select',
      options: [
        { value: 'active', label: 'status.active' },
        { value: 'disabled', label: 'status.disabled' },
      ],
    },
  ],
  statusActions: ['enable', 'disable'],
};

export default function ServicesPage() {
  return <CrudPage def={def} />;
}
