import { useTranslation } from 'react-i18next';
import CrudPage, { PageDef } from '../../components/CrudPage';
import { IdCell } from '../../components/admin/IdCell';
import { DetailGrid } from '../../components/admin/DetailGrid';

// 未绑定证书时的占位提示（需在组件内取 t，故单独成组件）。
function NoCertHint() {
  const { t } = useTranslation('admin');
  return (
    <div className="text-xs text-slate-400 dark:text-slate-500">{t('domains.noCert')}</div>
  );
}

const tlsLabel = (m?: string) => {
  switch (m) {
    case 'cloudflare': return 'Cloudflare SaaS';
    case 'managed': return 'Managed';
    case 'manual': return 'Direct TLS';
    default: return 'Disabled';
  }
};

const fmtTime = (v?: string) => (v ? new Date(v).toLocaleString() : '-');

// 证书列表中按 domain_id（优先）或 hostname 匹配该域名的证书。
const certFor = (certs: any[], r: any) =>
  certs.find((c) => c.domain_id === r.id) ?? certs.find((c) => c.hostname === r.hostname);

const def: PageDef = {
  title: 'domains.title',
  path: '/api/admin/domains',
  columns: [
    { key: 'id', label: 'fields.id', render: (r) => <IdCell id={r.id} /> },
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
  detailLoad: ['/api/admin/certificates'],
  renderDetail: (r, data) => {
    const cert = certFor(data['/api/admin/certificates'] ?? [], r);
    return (
      <div className="space-y-4">
        <DetailGrid
          title="domains.detailDomain"
          items={[
            { label: 'fields.id', value: r.id },
            { label: 'fields.hostname', value: r.hostname },
            { label: 'fields.tenant', value: r.tenant_id },
            { label: 'fields.service', value: r.service_id },
            { label: 'fields.tlsMode', value: tlsLabel(r.tls_mode) },
            { label: 'fields.status', value: r.status },
            { label: 'fields.certificateStatus', value: r.certificate_status },
            { label: 'fields.verifiedAt', value: fmtTime(r.verified_at) },
            { label: 'fields.createdAt', value: fmtTime(r.created_at) },
            { label: 'fields.updatedAt', value: fmtTime(r.updated_at) },
          ]}
        />
        {cert ? (
          <DetailGrid
            title="domains.detailCert"
            items={[
              { label: 'fields.status', value: cert.status },
              { label: 'fields.source', value: cert.source },
              { label: 'fields.issuer', value: cert.issuer },
              { label: 'fields.serialNumber', value: cert.serial_number },
              { label: 'fields.issuedAt', value: fmtTime(cert.issued_at) },
              { label: 'fields.expiresAt', value: fmtTime(cert.expires_at) },
              { label: 'certificates.lastRenewed', value: fmtTime(cert.last_renewed_at) },
              { label: 'certificates.nextRenew', value: fmtTime(cert.next_renew_at) },
              { label: 'fields.renewAttempts', value: cert.renew_attempts ?? 0 },
              { label: 'fields.lastError', value: cert.last_error || cert.last_renew_error },
            ]}
          />
        ) : (
          <NoCertHint />
        )}
      </div>
    );
  },
};

export default function DomainsPage() {
  return <CrudPage def={def} />;
}
