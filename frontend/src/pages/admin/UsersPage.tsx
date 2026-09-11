// 平台用户（管理员账号）管理页：列表 + 展开详情 + 新建 + 改角色 + 启停。
// 后端契约见 internal/api/admins.go（写操作经 RBAC 限 super_admin）。
import { Fragment, useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { ChevronDownIcon, ChevronRightIcon } from '@heroicons/react/24/outline';
import { list } from '../../lib/modules';
import { api } from '../../lib/client';
import { useAuth } from '../../stores/auth';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { DetailGrid } from '../../components/admin/DetailGrid';
import { IdCell } from '../../components/admin/IdCell';
import { PageHeader } from '../../themes';

// AdminUser 与 /api/admin/admins 返回字段对应。
interface AdminUser {
  id: string;
  email: string;
  name: string;
  role: string;
  status: string;
  created_at: string;
}

// 平台角色（与后端 auth.ValidRole 一致）。
const ROLES = ['super_admin', 'operator', 'viewer'] as const;

const roleColor: Record<string, string> = {
  super_admin: 'bg-violet-100 text-violet-700 dark:bg-violet-900/50 dark:text-violet-300',
  operator: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  viewer: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
};

const fmtTime = (v?: string) => (v ? new Date(v).toLocaleString() : '-');

const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';

export default function UsersPage() {
  const { t } = useTranslation('admin');
  const me = useAuth((s) => s.admin);
  const isSuper = me?.role === 'super_admin';

  const [rows, setRows] = useState<AdminUser[]>([]);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // 新建弹窗。
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState<string>('operator');

  const load = useCallback(async () => {
    try {
      setRows(await list<AdminUser>('/api/admin/admins'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
  }, [load]);

  const closeCreate = () => {
    setCreateOpen(false);
    setEmail('');
    setPassword('');
    setName('');
    setRole('operator');
  };

  const doCreate = async () => {
    if (!email) {
      toast.error(t('crud.requiredField', { field: t('fields.email') }));
      return;
    }
    if (password.length < 8) {
      toast.error(t('users.errPassword'));
      return;
    }
    setBusy(true);
    try {
      await api.post('/api/admin/admins', { email, password, name, role });
      toast.success(t('common.createSuccess'));
      closeCreate();
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  const changeRole = async (id: string, next: string) => {
    try {
      await api.patch(`/api/admin/admins/${id}/role`, { role: next });
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
      await load(); // 失败（如最后一个超管）回滚到实际值
    }
  };

  const toggleStatus = async (u: AdminUser) => {
    const next = u.status === 'active' ? 'disabled' : 'active';
    try {
      await api.patch(`/api/admin/admins/${u.id}/status`, { status: next });
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  return (
    <div>
      <PageHeader
        title={t('users.title')}
        right={
          isSuper ? (
            <button
              onClick={() => setCreateOpen(true)}
              className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
            >
              {`+ ${t('users.new')}`}
            </button>
          ) : undefined
        }
      />

      <Modal
        open={createOpen}
        title={t('users.new')}
        onClose={closeCreate}
        footer={
          <>
            <button
              onClick={closeCreate}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={doCreate}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {t('common.create')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('fields.email')}>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder={t('users.emailPh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('fields.name')}>
            <input value={name} onChange={(e) => setName(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('users.password')}>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={t('users.passwordPh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('fields.role')}>
            <select value={role} onChange={(e) => setRole(e.target.value)} className={inputCls}>
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {t(`users.role.${r}`)}
                </option>
              ))}
            </select>
          </Field>
        </div>
      </Modal>

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.email')}</th>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('fields.role')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('fields.createdAt')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                  {t('common.none')}
                </td>
              </tr>
            )}
            {rows.map((u) => {
              const expanded = expandedId === u.id;
              const isSelf = me?.id === u.id;
              return (
                <Fragment key={u.id}>
                  <tr className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                    <td className="px-4 py-2">
                      <span className="inline-flex items-center gap-1">
                        <button
                          type="button"
                          onClick={() => setExpandedId((cur) => (cur === u.id ? null : u.id))}
                          aria-label={expanded ? t('common.collapse') : t('common.expand')}
                          className="shrink-0 text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
                        >
                          {expanded ? (
                            <ChevronDownIcon className="h-4 w-4" />
                          ) : (
                            <ChevronRightIcon className="h-4 w-4" />
                          )}
                        </button>
                        {u.email}
                      </span>
                    </td>
                    <td className="px-4 py-2">{u.name || '-'}</td>
                    <td className="px-4 py-2">
                      {isSuper ? (
                        <select
                          value={u.role}
                          onChange={(e) => changeRole(u.id, e.target.value)}
                          className="rounded border border-slate-300 bg-white px-1.5 py-0.5 text-xs text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200"
                        >
                          {ROLES.map((r) => (
                            <option key={r} value={r}>
                              {t(`users.role.${r}`)}
                            </option>
                          ))}
                        </select>
                      ) : (
                        <span
                          className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${
                            roleColor[u.role] ?? roleColor.viewer
                          }`}
                        >
                          {t(`users.role.${u.role}`, { defaultValue: u.role })}
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-2">
                      <StatusBadge value={u.status} />
                    </td>
                    <td className="px-4 py-2 text-slate-500 dark:text-slate-400">
                      {fmtTime(u.created_at)}
                    </td>
                    <td className="px-4 py-2">
                      {isSuper && (
                        <ActionBtn onClick={() => toggleStatus(u)} disabled={isSelf}>
                          {u.status === 'active' ? t('common.disable') : t('common.enable')}
                        </ActionBtn>
                      )}
                    </td>
                  </tr>
                  {expanded && (
                    <tr className="border-b border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40">
                      <td colSpan={6} className="px-4 py-3">
                        <DetailGrid
                          items={[
                            { label: 'fields.id', value: <IdCell id={u.id} /> },
                            { label: 'fields.email', value: u.email },
                            { label: 'fields.name', value: u.name },
                            {
                              label: 'fields.role',
                              value: t(`users.role.${u.role}`, { defaultValue: u.role }),
                            },
                            { label: 'fields.status', value: u.status },
                            { label: 'fields.createdAt', value: fmtTime(u.created_at) },
                          ]}
                        />
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
