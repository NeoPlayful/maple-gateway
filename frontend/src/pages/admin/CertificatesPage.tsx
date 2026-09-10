import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { list, remove } from '../../lib/modules';
import { api } from '../../lib/client';
import { usePoll } from '../../lib/usePoll';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { ProgressStepper } from '../../components/admin/ProgressStepper';
import { PageHeader } from '../../themes';

// 签发/续期操作进度（后端 certificate_operations）。
interface CertificateOperation {
  id: string;
  hostname: string;
  domain_id?: string | null;
  action: string;
  status: string;
  message?: string;
  error?: string;
}

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
  last_renewed_at?: string | null;
  next_renew_at?: string | null;
  renew_attempts?: number;
  last_renew_error?: string;
  last_error?: string;
  created_at: string;
}

// Domain 用于编辑弹窗的换绑下拉。
interface Domain {
  id: string;
  hostname: string;
}

// 值语义：KEEP=保持原绑定（默认）、CLEAR=解绑、其余为具体域名 id。
const DOMAIN_KEEP = '__keep__';
const DOMAIN_CLEAR = '__clear__';

const domainName = (d?: string | null) => (d ? `${d.slice(0, 8)}…` : '-');

const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';
const pemCls = `${inputCls} font-mono text-xs`;

export default function CertificatesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Certificate[]>([]);
  const [domains, setDomains] = useState<Domain[]>([]);
  const [busy, setBusy] = useState(false);
  // 待删除证书 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 上传弹窗。
  const [uploadOpen, setUploadOpen] = useState(false);
  const [uploadDomain, setUploadDomain] = useState('');
  const [hostname, setHostname] = useState('');
  const [certPem, setCertPem] = useState('');
  const [keyPem, setKeyPem] = useState('');

  // 编辑弹窗（按 id 更换材料）：null=关闭。
  const [editing, setEditing] = useState<{ id: string; hostname: string } | null>(null);
  const [editCertPem, setEditCertPem] = useState('');
  const [editKeyPem, setEditKeyPem] = useState('');
  const [editDomain, setEditDomain] = useState(DOMAIN_KEEP);

  // 签发弹窗（Managed / ACME 自动签发）。
  const [issueOpen, setIssueOpen] = useState(false);
  const [issueDomain, setIssueDomain] = useState('');
  const [issueHost, setIssueHost] = useState('');

  // 签发进度：pollOpId 非空时轮询该操作；initialOp 为首个返回视图（轮询前展示）。
  const [pollOpId, setPollOpId] = useState<string | null>(null);
  const [initialOp, setInitialOp] = useState<CertificateOperation | null>(null);

  // 轮询签发进度，到终态（active/failed）停止。
  const polledOp = usePoll<CertificateOperation>(
    pollOpId,
    () => api.get<CertificateOperation>(`/api/admin/certificates/operations/${pollOpId}`),
    (v) => v.status === 'active' || v.status === 'failed',
  );
  const progressOp = polledOp ?? initialOp;

  const load = useCallback(async () => {
    try {
      setRows(await list<Certificate>('/api/admin/certificates'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  const loadDomains = useCallback(async () => {
    try {
      setDomains(await list<Domain>('/api/admin/domains'));
    } catch {
      setDomains([]);
    }
  }, []);

  useEffect(() => {
    load();
    loadDomains();
  }, [load, loadDomains]);

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      const res = (await remove('/api/admin/certificates', deleteId)) as { revoke_error?: string };
      if (res?.revoke_error) {
        // 本地已删除，但 CA 撤销失败——证书在 CA 侧可能仍有效，明确告警。
        toast.error(t('certificates.deleteRevokeFailed'));
      } else {
        toast.success(t('common.deleteSuccess'));
      }
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const doReload = async (id: string) => {
    try {
      await api.post(`/api/admin/certificates/${id}/reload`);
      toast.success(t('certificates.reloadSuccess'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('certificates.reloadFailed'));
    }
  };

  // 选择域名：以所选域名的 hostname 作为证书主机名（签发/上传共用）。
  const pickDomain = (id: string, apply: (hostname: string) => void) => {
    apply(domains.find((d) => d.id === id)?.hostname ?? '');
  };

  // 签发（Managed / ACME）：非阻塞受理，返回操作记录后打开进度弹窗轮询。
  const submitIssue = async () => {
    if (!issueDomain) {
      toast.error(t('certificates.errPickDomain'));
      return;
    }
    if (!issueHost.trim()) {
      toast.error(t('certificates.errFill'));
      return;
    }
    setBusy(true);
    try {
      const op = await api.post<CertificateOperation>('/api/admin/certificates/issue', {
        hostname: issueHost.trim(),
        domain_id: issueDomain,
        source: 'acme',
      });
      setIssueOpen(false);
      setIssueDomain('');
      setIssueHost('');
      if (op.status === 'active') {
        // 未启用异步运行时：同步完成。
        toast.success(t('certificates.issueSuccess'));
        await load();
      } else {
        setInitialOp(op);
        setPollOpId(op.id);
      }
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('certificates.issueFailed'));
    } finally {
      setBusy(false);
    }
  };

  // 进度到终态：提示并刷新列表；弹窗保留终态展示，由用户手动关闭。
  const polledStatus = polledOp?.status;
  useEffect(() => {
    if (polledStatus === 'active') {
      toast.success(t('certificates.issueSuccess'));
      load();
    } else if (polledStatus === 'failed') {
      toast.error(polledOp?.error || t('certificates.issueFailed'));
      load();
    }
  }, [polledStatus, polledOp?.error, t, load]);

  // 关闭进度弹窗：停止轮询并清空。
  const closeProgress = () => {
    setPollOpId(null);
    setInitialOp(null);
  };

  // 立即续期（仅 ACME 来源有效）。
  const doRenew = async (id: string) => {
    try {
      await api.post(`/api/admin/certificates/${id}/renew`);
      toast.success(t('certificates.renewSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('certificates.renewFailed'));
    }
  };

  const closeUpload = () => {
    setUploadOpen(false);
  };

  const submitUpload = async () => {
    if (!hostname.trim() || !certPem.trim() || !keyPem.trim()) {
      toast.error(t('certificates.errFill'));
      return;
    }
    setBusy(true);
    try {
      await api.post('/api/admin/certificates', {
        hostname: hostname.trim(),
        certificate_pem: certPem.trim(),
        private_key_pem: keyPem.trim(),
        domain_id: uploadDomain || undefined,
      });
      toast.success(t('certificates.uploadSuccess'));
      setUploadOpen(false);
      setUploadDomain('');
      setHostname('');
      setCertPem('');
      setKeyPem('');
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('certificates.uploadFailed'));
    } finally {
      setBusy(false);
    }
  };

  // 打开编辑弹窗：hostname 只读回填，材料留空待重填，域名默认保持原绑定。
  const openEdit = (r: Certificate) => {
    setEditing({ id: r.id, hostname: r.hostname });
    setEditCertPem('');
    setEditKeyPem('');
    setEditDomain(DOMAIN_KEEP);
  };

  const closeEdit = () => {
    setEditing(null);
  };

  const submitEdit = async () => {
    if (!editing) return;
    const cert = editCertPem.trim();
    const key = editKeyPem.trim();
    // 材料两态：都填=替换材料；都空=仅改绑定；只填其一非法。
    if ((cert === '') !== (key === '')) {
      toast.error(t('certificates.errMaterialPair'));
      return;
    }
    const body: Record<string, unknown> = {};
    if (cert !== '') {
      body.certificate_pem = cert;
      body.private_key_pem = key;
    }
    // 域名三态：保持 → 不发字段；解绑 → domain_id_clear；指定 → domain_id。
    if (editDomain === DOMAIN_CLEAR) body.domain_id_clear = true;
    else if (editDomain !== DOMAIN_KEEP) body.domain_id = editDomain;

    setBusy(true);
    try {
      await api.patch(`/api/admin/certificates/${editing.id}`, body);
      toast.success(t('certificates.editSuccess'));
      setEditing(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('certificates.editFailed'));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div>
      <PageHeader
        title={t('certificates.title')}
        right={
          <div className="flex gap-2">
            <button
              onClick={() => {
                setIssueDomain('');
                setIssueHost('');
                setIssueOpen(true);
              }}
              className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
            >
              {`+ ${t('certificates.issue')}`}
            </button>
            <button
              onClick={() => {
                setUploadDomain('');
                setHostname('');
                setUploadOpen(true);
              }}
              className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {`+ ${t('certificates.upload')}`}
            </button>
          </div>
        }
      />
      <Modal
        open={uploadOpen}
        title={t('certificates.upload')}
        onClose={closeUpload}
        footer={
          <>
            <button
              onClick={closeUpload}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={submitUpload}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('certificates.uploading') : t('certificates.upload')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('certificates.domainLabel')}>
            <select
              value={uploadDomain}
              onChange={(e) => {
                const id = e.target.value;
                setUploadDomain(id);
                pickDomain(id, setHostname);
              }}
              className={inputCls}
            >
              <option value="">{t('certificates.domainNoBind')}</option>
              {domains.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.hostname}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('certificates.hostname')}>
            <input
              value={hostname}
              onChange={(e) => setHostname(e.target.value)}
              placeholder={t('certificates.hostnamePh')}
              className={inputCls}
            />
          </Field>
        </div>
        <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Field label={t('certificates.certPem')}>
            <textarea
              value={certPem}
              onChange={(e) => setCertPem(e.target.value)}
              placeholder={t('certificates.certPemPh')}
              rows={8}
              spellCheck={false}
              className={pemCls}
            />
          </Field>
          <Field label={t('certificates.keyPem')}>
            <textarea
              value={keyPem}
              onChange={(e) => setKeyPem(e.target.value)}
              placeholder={t('certificates.keyPemPh')}
              rows={8}
              spellCheck={false}
              className={pemCls}
            />
          </Field>
        </div>
      </Modal>

      <Modal
        open={issueOpen}
        title={t('certificates.issueTitle')}
        onClose={() => setIssueOpen(false)}
        footer={
          <>
            <button
              onClick={() => setIssueOpen(false)}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={submitIssue}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('certificates.issuing') : t('certificates.issue')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('certificates.domainLabel')}>
            <select
              value={issueDomain}
              onChange={(e) => {
                const id = e.target.value;
                setIssueDomain(id);
                pickDomain(id, setIssueHost);
              }}
              className={inputCls}
            >
              <option value="">{t('common.select')}</option>
              {domains.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.hostname}
                </option>
              ))}
            </select>
          </Field>
          <Field label={t('certificates.hostname')}>
            <input value={issueHost} readOnly disabled className={`${inputCls} opacity-60`} />
          </Field>
        </div>
        <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">{t('certificates.issueHint')}</p>
      </Modal>

      <Modal
        open={editing !== null}
        title={t('certificates.editTitle')}
        onClose={closeEdit}
        footer={
          <>
            <button
              onClick={closeEdit}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={submitEdit}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving', '保存中…') : t('common.save')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('certificates.hostname')}>
            <input value={editing?.hostname ?? ''} readOnly disabled className={`${inputCls} opacity-60`} />
          </Field>
          <Field label={t('certificates.domainLabel')}>
            <select
              value={editDomain}
              onChange={(e) => setEditDomain(e.target.value)}
              className={inputCls}
            >
              <option value={DOMAIN_KEEP}>{t('certificates.domainKeep')}</option>
              <option value={DOMAIN_CLEAR}>{t('certificates.domainClear')}</option>
              {domains.map((d) => (
                <option key={d.id} value={d.id}>
                  {d.hostname}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
          {t('certificates.editHint')}
        </p>
        <div className="mt-3 grid grid-cols-1 gap-3 lg:grid-cols-2">
          <Field label={t('certificates.certPem')}>
            <textarea
              value={editCertPem}
              onChange={(e) => setEditCertPem(e.target.value)}
              placeholder={t('certificates.certPemPh')}
              rows={8}
              spellCheck={false}
              className={pemCls}
            />
          </Field>
          <Field label={t('certificates.keyPem')}>
            <textarea
              value={editKeyPem}
              onChange={(e) => setEditKeyPem(e.target.value)}
              placeholder={t('certificates.keyPemPh')}
              rows={8}
              spellCheck={false}
              className={pemCls}
            />
          </Field>
        </div>
      </Modal>

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle')}
        message={t('certificates.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />

      <Modal
        open={progressOp !== null}
        title={t('certificates.issueProgressTitle')}
        onClose={closeProgress}
        footer={
          <button
            onClick={closeProgress}
            className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
          >
            {t('common.close')}
          </button>
        }
      >
        {progressOp && (
          <>
            <div className="mb-3 font-mono text-sm text-slate-700 dark:text-slate-300">
              {progressOp.hostname}
            </div>
            <ProgressStepper
              status={progressOp.status}
              message={progressOp.message}
              error={progressOp.error}
            />
          </>
        )}
      </Modal>

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.hostname')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('fields.issuer')}</th>
              <th className="px-4 py-2">{t('fields.source')}</th>
              <th className="px-4 py-2">{t('fields.expiresAt')}</th>
              <th className="px-4 py-2">{t('certificates.nextRenew')}</th>
              <th className="px-4 py-2">{t('fields.domainId')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
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
                <td className="px-4 py-2">
                  {r.next_renew_at ? new Date(r.next_renew_at).toLocaleString() : '-'}
                  {r.renew_attempts ? (
                    <span className="ml-1 text-xs text-amber-600" title={r.last_renew_error}>
                      {`↻${r.renew_attempts}`}
                    </span>
                  ) : null}
                </td>
                <td className="px-4 py-2">{domainName(r.domain_id)}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    <ActionBtn onClick={() => doReload(r.id)}>{t('certificates.reload')}</ActionBtn>
                    {r.source === 'acme' && (
                      <ActionBtn onClick={() => doRenew(r.id)}>{t('certificates.renew')}</ActionBtn>
                    )}
                    <ActionBtn danger onClick={() => setDeleteId(r.id)}>
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
