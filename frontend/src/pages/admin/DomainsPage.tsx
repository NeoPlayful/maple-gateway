import CrudPage, { PageDef } from '../../components/CrudPage';

const tlsLabel = (m?: string) => {
  switch (m) {
    case 'cloudflare': return 'Cloudflare SaaS';
    case 'managed': return 'Managed';
    case 'manual': return 'Direct TLS';
    default: return 'Disabled';
  }
};

const def: PageDef = {
  title: 'domains.title',
  path: '/api/admin/domains',
  columns: [
    { key: 'hostname', label: 'fields.hostname' },
    { key: 'tenant_id', label: 'fields.tenant', render: (r) => r.tenant_id?.slice(0, 8) ?? '-' },
    { key: 'tls_mode', label: 'fields.tlsMode', render: (r) => tlsLabel(r.tls_mode) },
    { key: 'certificate_status', label: 'fields.certificateStatus', render: (r) => r.certificate_status ?? '-' },
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
  editFields: [
    { key: 'hostname', label: 'fields.hostname', required: true, placeholder: 'domains.hostnamePh' },
    {
      key: 'tls_mode',
      label: 'fields.tlsMode',
      type: 'select',
      options: [
        { value: 'cloudflare', label: 'domains.tlsCloudflare' },
        { value: 'managed', label: 'domains.tlsManaged' },
        { value: 'manual', label: 'domains.tlsManual' },
        { value: 'disabled', label: 'domains.tlsDisabled' },
      ],
    },
    {
      key: 'status',
      label: 'fields.status',
      type: 'select',
      options: [
        { value: 'active', label: 'status.active' },
        { value: 'disabled', label: 'status.disabled' },
        { value: 'pending', label: 'status.pending' },
      ],
    },
    {
      key: 'service_id',
      label: 'fields.service',
      type: 'select',
      loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' },
      clearOnEmpty: true,
    },
  ],
  statusActions: ['enable', 'disable'],
};

export default function DomainsPage() {
  return <CrudPage def={def} />;
}
