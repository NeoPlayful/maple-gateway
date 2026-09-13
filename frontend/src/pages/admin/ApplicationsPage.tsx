import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api, ApiError } from '../../lib/client';
import type { CMApplication, CMAppService } from '../../types';
import { PageHeader } from '../../themes';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';

// 一个可直接套用的最小 Compose 规格示例，帮助首次使用者快速上手。
const SAMPLE_SPEC = `services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
`;

export default function ApplicationsPage() {
  const { t } = useTranslation('admin');
  const [apps, setApps] = useState<CMApplication[]>([]);
  const [enabled, setEnabled] = useState(true);
  const [err, setErr] = useState('');
  const [last, setLast] = useState('');

  // 编辑弹窗（新建 / 编辑复用）。
  const [editing, setEditing] = useState<CMApplication | null>(null);
  const [form, setForm] = useState({ id: '', name: '', description: '', version: '', spec: '' });
  const [formErr, setFormErr] = useState('');
  const [saving, setSaving] = useState(false);

  // 服务详情弹窗。
  const [detail, setDetail] = useState<{ app: CMApplication; services: CMAppService[] } | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const [removeId, setRemoveId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await api.get<CMApplication[]>('/api/admin/cm/applications');
      setApps(list ?? []);
      setEnabled(true);
      setErr('');
      setLast(new Date().toLocaleTimeString());
    } catch (e) {
      if (e instanceof ApiError && e.status === 503) {
        setEnabled(false);
        setErr('');
      } else {
        setErr(e instanceof Error ? e.message : t('common.loadFailed'));
      }
    }
  }, [t]);

  useEffect(() => {
    load();
    const timer = setInterval(load, 10000);
    return () => clearInterval(timer);
  }, [load]);

  const openNew = () => {
    setEditing({} as CMApplication);
    setForm({ id: '', name: '', description: '', version: 'v1', spec: SAMPLE_SPEC });
    setFormErr('');
  };

  const openEdit = (a: CMApplication) => {
    setEditing(a);
    setForm({ id: a.id, name: a.name, description: a.description ?? '', version: a.version ?? '', spec: a.spec ?? '' });
    setFormErr('');
  };

  const submit = async () => {
    setFormErr('');
    if (!form.name.trim()) {
      setFormErr(t('applications.errName'));
      return;
    }
    if (!form.spec.trim()) {
      setFormErr(t('applications.errSpec'));
      return;
    }
    setSaving(true);
    try {
      await api.post('/api/admin/cm/applications', {
        id: form.id,
        name: form.name,
        description: form.description,
        version: form.version,
        spec: form.spec,
      });
      toast.success(t('common.updateSuccess'));
      setEditing(null);
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.updateFailed'));
    } finally {
      setSaving(false);
    }
  };

  const act = async (id: string, a: string) => {
    try {
      await api.post(`/api/admin/cm/applications/${id}/${a}`);
      toast.success(t('common.operateSuccess'));
      await load();
      if (detail?.app.id === id) openDetail(detail.app);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const validate = async (id: string) => {
    try {
      const res = await api.post<{ valid: boolean; output?: string }>(`/api/admin/cm/applications/${id}/validate`);
      if (res.valid) toast.success(t('applications.validateOk'));
      else toast.error(`${t('applications.validateFail')}: ${res.output ?? ''}`);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const openDetail = async (a: CMApplication) => {
    setDetail({ app: a, services: [] });
    setDetailLoading(true);
    try {
      const res = await api.get<{ services: CMAppService[] }>(`/api/admin/cm/applications/${a.id}/ps`);
      setDetail({ app: a, services: res.services ?? [] });
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
      setDetail({ app: a, services: [] });
    } finally {
      setDetailLoading(false);
    }
  };

  const doRemove = async () => {
    if (!removeId) return;
    try {
      await api.delete(`/api/admin/cm/applications/${removeId}`);
      toast.success(t('common.deleteSuccess'));
      setRemoveId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const sorted = useMemo(
    () => [...apps].sort((a, b) => (a.created_at < b.created_at ? 1 : -1)),
    [apps],
  );

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';
  const taCls = `${inputCls} font-mono text-xs`;

  if (!enabled) {
    return (
      <div>
        <PageHeader title={t('applications.title')} />
        <p className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-300">
          {t('runtime.notEnabled')}
        </p>
      </div>
    );
  }

  return (
    <div>
      <PageHeader
        title={t('applications.title')}
        right={
          <div className="flex items-center gap-2 text-xs text-slate-400">
            <button onClick={load} className="rounded bg-slate-200 px-3 py-1.5 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">{t('common.refresh')}</button>
            {last && <span>{last}</span>}
            <button onClick={openNew} className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover">+ {t('applications.new')}</button>
          </div>
        }
      />
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('applications.colVersion')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('runtime.colNode')}</th>
              <th className="px-4 py-2">{t('applications.colUpdated')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {sorted.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('applications.none')}</td>
              </tr>
            )}
            {sorted.map((a) => (
              <tr key={a.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2">
                  <button onClick={() => openDetail(a)} className="font-semibold text-sky-600 hover:underline dark:text-sky-400">{a.name}</button>
                  {a.description && <p className="text-xs text-slate-400">{a.description}</p>}
                </td>
                <td className="px-4 py-2 font-mono text-xs">{a.version || '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={a.status} raw /></td>
                <td className="px-4 py-2 font-mono text-xs">{a.node_id ? a.node_id.slice(0, 8) : '-'}</td>
                <td className="px-4 py-2 text-xs text-slate-400">{a.updated_at ? new Date(a.updated_at).toLocaleString() : '-'}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => validate(a.id)}>{t('applications.validate')}</ActionBtn>
                    <ActionBtn onClick={() => act(a.id, 'deploy')}>{t('applications.deploy')}</ActionBtn>
                    <ActionBtn onClick={() => act(a.id, 'restart')}>{t('applications.restart')}</ActionBtn>
                    {a.status === 'running' && <ActionBtn onClick={() => act(a.id, 'stop')}>{t('applications.stop')}</ActionBtn>}
                    <ActionBtn onClick={() => openEdit(a)}>{t('common.edit')}</ActionBtn>
                    <ActionBtn danger onClick={() => setRemoveId(a.id)}>{t('common.delete')}</ActionBtn>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* 新建 / 编辑弹窗 */}
      <Modal
        open={editing !== null}
        title={form.id ? t('applications.editTitle') : t('applications.createTitle')}
        onClose={() => setEditing(null)}
        maxWidth="max-w-3xl"
        footer={
          <>
            <button onClick={() => setEditing(null)} className="rounded border border-slate-300 px-4 py-1.5 text-sm text-slate-600 hover:bg-slate-50 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-700">{t('common.cancel')}</button>
            <button onClick={submit} disabled={saving} className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-50">{saving ? t('common.saving') : t('common.save')}</button>
          </>
        }
      >
        <div className="space-y-3">
          {formErr && <p className="rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{formErr}</p>}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.name')}</label>
              <input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder={t('applications.namePh')} className={inputCls} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('applications.colVersion')}</label>
              <input value={form.version} onChange={(e) => setForm({ ...form, version: e.target.value })} placeholder="v1" className={inputCls} />
            </div>
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('fields.description')}</label>
            <input value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} className={inputCls} />
          </div>
          <div>
            <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('applications.specLabel')}</label>
            <textarea value={form.spec} onChange={(e) => setForm({ ...form, spec: e.target.value })} rows={12} spellCheck={false} className={taCls} />
            <p className="mt-1 text-xs text-slate-400">{t('applications.specHint')}</p>
          </div>
        </div>
      </Modal>

      {/* 服务详情弹窗 */}
      <Modal
        open={detail !== null}
        title={`${t('applications.services')} · ${detail?.app.name ?? ''}`}
        onClose={() => setDetail(null)}
        maxWidth="max-w-4xl"
      >
        {detailLoading ? (
          <p className="text-sm text-slate-400">{t('common.loading')}</p>
        ) : detail && detail.services.length === 0 ? (
          <p className="text-sm text-slate-400">{t('applications.noServices')}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
                <tr>
                  <th className="px-3 py-2">{t('applications.colService')}</th>
                  <th className="px-3 py-2">{t('fields.status')}</th>
                  <th className="px-3 py-2">{t('applications.colHealth')}</th>
                  <th className="px-3 py-2">{t('runtime.colImage')}</th>
                  <th className="px-3 py-2">{t('applications.colPorts')}</th>
                </tr>
              </thead>
              <tbody>
                {detail?.services.map((s, i) => (
                  <tr key={`${s.name}-${i}`} className="border-b border-slate-200 last:border-b-0 dark:border-slate-700">
                    <td className="px-3 py-2 font-semibold text-slate-700 dark:text-slate-200">{s.service || s.name}</td>
                    <td className="px-3 py-2"><StatusBadge value={s.state} raw /></td>
                    <td className="px-3 py-2 text-xs">{s.health || '-'}</td>
                    <td className="px-3 py-2 font-mono text-xs">{s.image || '-'}</td>
                    <td className="px-3 py-2 font-mono text-xs">
                      {(s.publishers ?? []).map((p) => `${p.published_port ?? ''}→${p.target_port ?? ''}`).join(', ') || '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Modal>

      <ConfirmDialog
        open={removeId !== null}
        title={t('common.confirmTitle')}
        message={t('applications.confirmRemove')}
        onConfirm={doRemove}
        onCancel={() => setRemoveId(null)}
      />
    </div>
  );
}
