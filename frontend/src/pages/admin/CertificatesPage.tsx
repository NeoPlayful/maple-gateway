import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { list, remove } from '../../lib/modules';
import { api } from '../../lib/client';
import { StatusBadge, ActionBtn, Field } from '../../components/ui';
import { PageHeader } from '../../themes';

// Certificate 与后端 /api/admin/certificates 返回字段对应。
interface Certificate {
  id: string;
  domain_id?: string | null;
  hostname: string;
  source?: string;
  status: string;
  issuer?: string;
  serial_number?: string;
  issued_at?: string | null;
  expires_at?: string | null;
  last_error?: string;
  created_at: string;
}

// upload 是 New 结构（certificate_pem/private_key_pem 独立于返回模型）。
interface CertificateDraft {
  hostname: string;
  certificate_pem: string;
  private_key_pem: string;
}

const domainName = (d?: string | null) => (d ? `${d.slice(0, 8)}…` : '-');

export default function CertificatesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Certificate[]>([]);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [open, setOpen] = useState(false);
  const [hostname, setHostname] = useState('');
  const [certPem, setCertPem] = useState('');
  const [keyPem, setKeyPem] = useState('');
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setRows(await list<Certificate>('/api/admin/certificates'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const doDelete = async (id: string) => {
    if (!window.confirm(t('certificates.confirmDelete'))) return;
    setErr('');
    setMsg('');
    try {
      await remove('/api/admin/certificates', id);
      setMsg(t('common.deleteSuccess'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const doReload = async (id: string) => {
    setErr('');
    setMsg('');
    try {
      await api.post(`/api/admin/certificates/${id}/reload`);
      setMsg(t('certificates.reloadSuccess'));
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('certificates.reloadFailed'));
    }
  };

  const submit = async () => {
    setErr('');
    if (!hostname.trim() || !certPem.trim() || !keyPem.trim()) {
      setErr(t('certificates.errFill'));
      return;
    }
    const body: CertificateDraft = {
      hostname: hostname.trim(),
      certificate_pem: certPem.trim(),
      private_key_pem: keyPem.trim(),
    };
    setBusy(true);
    try {
      await api.post('/api/admin/certificates', body);
      setMsg(t('certificates.uploadSuccess'));
      setOpen(false);
      setHostname('');
      setCertPem('');
      setKeyPem('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('certificates.uploadFailed'));
    } finally {
      setBusy(false);
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <PageHeader
        title={t('certificates.title')}
        right={
          <button
            onClick={() => setOpen((v) => !v)}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            {open ? t('common.collapse') : `+ ${t('certificates.upload')}`}
          </button>
        }
      />
      {msg && (
        <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>
      )}
      {err && (
        <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
          {err}
        </p>
      )}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <Field label={t('certificates.hostname')}>
            <input
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder={t('certificates.hostnamePh')}
              className={inputCls}
            />
          </Field>
          <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-2">
            <Field label={t('certificates.certPem')}>
              <textarea
                value={certPem}
                onChange={(e) => setCertPem(e.target.value)}
                placeholder={t('certificates.certPemPh')}
                rows={8}
                spellCheck={false}
                className={`${inputCls} font-mono text-xs`}
              />
            </Field>
            <Field label={t('certificates.keyPem')}>
              <textarea
                value={keyPem}
                onChange={(e) => setKeyPem(e.target.value)}
                placeholder={t('certificates.keyPemPh')}
                rows={8}
                spellCheck={false}
                className={`${inputCls} font-mono text-xs`}
              />
            </Field>
          </div>
          <button
            onClick={submit}
            disabled={busy}
            className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
          >
            {busy ? t('certificates.uploading') : t('certificates.upload')}
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.hostname')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('fields.issuer')}</th>
              <th className="px-4 py-2">{t('fields.source')}</th>
              <th className="px-4 py-2">{t('fields.expiresAt')}</th>
              <th className="px-4 py-2">{t('fields.domainId')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                  {t('certificates.none')}
                </td>
              </tr>
            )}
            {rows.map((r) => (
              <tr
                key={r.id}
                className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40"
              >
                <td className="px-4 py-2 font-mono text-xs">{r.hostname}</td>
                <td className="px-4 py-2">
                  <StatusBadge value={r.status} />
                  {r.last_error ? (
                    <span className="ml-1 text-xs text-rose-500" title={r.last_error}>
                      !
                    </span>
                  ) : null}
                </td>
                <td className="px-4 py-2">{r.issuer || '-'}</td>
                <td className="px-4 py-2">{r.source || '-'}</td>
                <td className="px-4 py-2">{r.expires_at ? new Date(r.expires_at).toLocaleString() : '-'}</td>
                <td className="px-4 py-2">{domainName(r.domain_id)}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => doReload(r.id)}>{t('certificates.reload')}</ActionBtn>
                    <ActionBtn danger onClick={() => doDelete(r.id)}>
                      {t('common.delete')}
                    </ActionBtn>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
