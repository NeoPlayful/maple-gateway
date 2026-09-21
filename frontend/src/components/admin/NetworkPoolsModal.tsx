// 节点网络池管理弹窗：列出该节点的项目级 IPAM 池，支持新增/编辑/排空/停用/复检/删除。
// 扩容方式是「新增池」而非改已有池网段（见文档 §7/§25）。
import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import type { NetworkPool } from '../../types';
import { Modal } from './Modal';
import { ActionBtn } from './ActionBtn';
import { Field } from './Field';
import { ConfirmDialog } from './ConfirmDialog';
import { StatusBadge } from './StatusBadge';

const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

// 空表单。
const EMPTY = { name: '', address_pool: '', project_prefix: '24', priority: '100' };

export function NetworkPoolsModal({
  open,
  nodeId,
  nodeName,
  onClose,
}: {
  open: boolean;
  nodeId: string;
  nodeName: string;
  onClose: () => void;
}) {
  const { t } = useTranslation('admin');
  const [pools, setPools] = useState<NetworkPool[]>([]);
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ ...EMPTY });
  const [editingId, setEditingId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const base = `/api/admin/cm/nodes/${nodeId}/network-pools`;

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setPools((await api.get<NetworkPool[]>(base)) ?? []);
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [base, t]);

  useEffect(() => {
    if (open && nodeId) load();
  }, [open, nodeId, load]);

  const resetForm = () => {
    setForm({ ...EMPTY });
    setEditingId(null);
    setFormErr('');
  };

  // 实时容量：2^(projectPrefix - poolPrefix)，仅当前缀合法时显示。
  const capacity = (() => {
    const prefix = Number(form.project_prefix);
    const m = form.address_pool.match(/\/(\d+)$/);
    if (!m) return null;
    const poolBits = Number(m[1]);
    if (!prefix || prefix <= poolBits || prefix > 30) return null;
    return 2 ** (prefix - poolBits);
  })();

  const submit = async () => {
    setFormErr('');
    if (!form.name.trim() || !form.address_pool.trim() || !form.project_prefix) {
      setFormErr(t('nodes.poolErrFill'));
      return;
    }
    setBusy(true);
    try {
      if (editingId) {
        await api.patch(`${base}/${editingId}`, {
          name: form.name.trim(),
          priority: Number(form.priority) || 100,
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await api.post(base, {
          name: form.name.trim(),
          address_pool: form.address_pool.trim(),
          project_prefix: Number(form.project_prefix),
          priority: Number(form.priority) || 100,
        });
        toast.success(t('common.createSuccess'));
      }
      resetForm();
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : t('common.operateFailed'));
    } finally {
      setBusy(false);
    }
  };

  // 状态流转：排空/停用/启用。
  const setStatus = async (id: string, status: string) => {
    try {
      await api.patch(`${base}/${id}`, { status });
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const recheck = async (id: string) => {
    try {
      await api.post(`${base}/${id}/check`);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await api.delete(`${base}/${deleteId}`);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const openEdit = (p: NetworkPool) => {
    setEditingId(p.id);
    setForm({
      name: p.name,
      address_pool: p.address_pool,
      project_prefix: String(p.project_prefix),
      priority: String(p.priority),
    });
    setFormErr('');
  };

  return (
    <Modal open={open} title={t('nodes.poolsTitle', { node: nodeName })} onClose={onClose} maxWidth="max-w-5xl">
      <div className="mb-3 rounded border border-slate-200 p-3 dark:border-slate-700">
        <div className="mb-2 text-xs text-slate-500 dark:text-slate-400">{t('nodes.poolHint')}</div>
        {formErr && (
          <p className="mb-2 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
            {formErr}
          </p>
        )}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-5">
          <Field label={t('nodes.poolName')}>
            <input
              value={form.name}
              onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              placeholder={t('nodes.poolNamePh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('nodes.poolCidr')}>
            <input
              value={form.address_pool}
              disabled={!!editingId}
              onChange={(e) => setForm((f) => ({ ...f, address_pool: e.target.value }))}
              placeholder={t('nodes.poolCidrPh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('nodes.poolPrefix')}>
            <input
              type="number"
              value={form.project_prefix}
              disabled={!!editingId}
              onChange={(e) => setForm((f) => ({ ...f, project_prefix: e.target.value }))}
              className={inputCls}
            />
          </Field>
          <Field label={t('nodes.poolPriority')}>
            <input
              type="number"
              value={form.priority}
              onChange={(e) => setForm((f) => ({ ...f, priority: e.target.value }))}
              className={inputCls}
            />
          </Field>
          <div className="flex items-end gap-2">
            <button
              onClick={submit}
              disabled={busy}
              className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-50"
            >
              {editingId ? t('common.save') : t('nodes.poolAdd')}
            </button>
            {editingId && (
              <button
                onClick={resetForm}
                className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200"
              >
                {t('common.cancel')}
              </button>
            )}
          </div>
        </div>
        {capacity !== null && (
          <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
            {t('nodes.poolCapacityCalc', { n: capacity })}
          </p>
        )}
      </div>

      <div className="overflow-x-auto rounded border border-slate-200 dark:border-slate-700">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-3 py-2">{t('nodes.poolName')}</th>
              <th className="px-3 py-2">{t('nodes.poolCidr')}</th>
              <th className="px-3 py-2">{t('nodes.poolPrefix')}</th>
              <th className="px-3 py-2">{t('nodes.poolPriority')}</th>
              <th className="px-3 py-2">{t('nodes.poolStatus')}</th>
              <th className="px-3 py-2">{t('nodes.poolCapacity')}</th>
              <th className="px-3 py-2">{t('nodes.poolAllocated')}</th>
              <th className="px-3 py-2">{t('nodes.poolAvailable')}</th>
              <th className="px-3 py-2">{t('nodes.poolUsage')}</th>
              <th className="px-3 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {pools.length === 0 && (
              <tr>
                <td colSpan={10} className="px-3 py-6 text-center text-slate-400 dark:text-slate-500">
                  {loading ? t('common.loading') : t('nodes.poolsEmpty')}
                </td>
              </tr>
            )}
            {pools.map((p) => (
              <tr key={p.id} className="border-b border-slate-200 dark:border-slate-700">
                <td className="px-3 py-2">
                  {p.name}
                  {p.is_system_default && <span className="ml-1 text-xs text-slate-400 dark:text-slate-500">(default)</span>}
                  {p.last_conflict_reason && (
                    <div className="text-xs text-rose-500 dark:text-rose-400">{p.last_conflict_reason}</div>
                  )}
                </td>
                <td className="px-3 py-2 font-mono text-xs">{p.address_pool}</td>
                <td className="px-3 py-2 font-mono text-xs">/{p.project_prefix}</td>
                <td className="px-3 py-2 font-mono text-xs">{p.priority}</td>
                <td className="px-3 py-2"><StatusBadge value={p.status} /></td>
                <td className="px-3 py-2 font-mono text-xs">{p.capacity}</td>
                <td className="px-3 py-2 font-mono text-xs">{p.allocated}</td>
                <td className="px-3 py-2 font-mono text-xs">{p.available}</td>
                <td className="px-3 py-2 font-mono text-xs">{(p.usage * 100).toFixed(2)}%</td>
                <td className="px-3 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(p)}>{t('nodes.poolEdit')}</ActionBtn>
                    {p.status === 'active' ? (
                      <ActionBtn onClick={() => setStatus(p.id, 'draining')}>{t('nodes.poolDrain')}</ActionBtn>
                    ) : (
                      <ActionBtn onClick={() => setStatus(p.id, 'active')}>{t('nodes.poolEnable')}</ActionBtn>
                    )}
                    <ActionBtn onClick={() => recheck(p.id)}>{t('nodes.poolRecheck')}</ActionBtn>
                    <ActionBtn danger onClick={() => setDeleteId(p.id)}>{t('nodes.poolDelete')}</ActionBtn>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle')}
        message={t('common.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </Modal>
  );
}
