// 平台用户（管理员账号）管理页：列表 + 展开详情 + 新建 + 改角色 + 启停。
// 后端契约见 internal/api/users.go（写操作经 RBAC 限 super_admin）。
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { ChevronDownIcon, ChevronRightIcon } from '@heroicons/react/24/outline';
import { list, remove } from '../../lib/modules';
import { api } from '../../lib/client';
import { useAuth } from '../../stores/auth';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { DetailGrid } from '../../components/admin/DetailGrid';
import { IdCell } from '../../components/admin/IdCell';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';

// AdminUser 与 /api/admin/users 返回字段对应。
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

// 筛选栏：关键字（邮箱/姓名）+ 角色 + 状态，三者 AND，纯前端过滤已加载列表。
const USER_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['email', 'name'], placeholder: 'users.filterPh' },
  { type: 'select', key: 'role', placeholder: 'common.filterAllRole', options: ROLES.map((r) => ({ value: r, label: `users.role.${r}` })) },
  {
    type: 'select',
    key: 'status',
    placeholder: 'common.filterAllStatus',
    options: [
      { value: 'active', label: 'status.active' },
      { value: 'disabled', label: 'status.disabled' },
    ],
  },
];

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

  // 列表筛选：关键字（邮箱/姓名）/ 角色 / 状态，三者 AND，纯前端过滤已加载列表。
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 新建弹窗。
  const [createOpen, setCreateOpen] = useState(false);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState<string>('operator');

  // 编辑弹窗：editing 非空=编辑（改 name/role，email 只读）。
  const [editing, setEditing] = useState<AdminUser | null>(null);
  const [editName, setEditName] = useState('');
  const [editRole, setEditRole] = useState('operator');

  // 待删除用户 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setRows(await list<AdminUser>('/api/admin/users'));
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
      await api.post('/api/admin/users', { email, password, name, role });
      toast.success(t('common.createSuccess'));
      closeCreate();
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  const openEdit = (u: AdminUser) => {
    setEditing(u);
    setEditName(u.name ?? '');
    setEditRole(u.role);
  };

  const closeEdit = () => {
    setEditing(null);
  };

  const doEdit = async () => {
    if (!editing) return;
    setBusy(true);
    try {
      await api.patch(`/api/admin/users/${editing.id}`, { name: editName, role: editRole });
      toast.success(t('common.updateSuccess'));
      setEditing(null);
      await load();
    } catch (e) {
      // 失败（如降级最后一名超管）保持弹窗打开并提示原因。
      toast.error(e instanceof Error ? e.message : t('common.updateFailed'));
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove('/api/admin/users', deleteId);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const toggleStatus = async (u: AdminUser) => {
    const next = u.status === 'active' ? 'disabled' : 'active';
    try {
      await api.patch(`/api/admin/users/${u.id}/status`, { status: next });
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const filtered = useMemo(
    () => applyFilters(rows, USER_FILTERS, filterValues),
    [rows, filterValues],
  );
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

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

      <Modal
        open={editing !== null}
        title={t('users.editTitle')}
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
              onClick={doEdit}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : t('common.save')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('fields.email')}>
            <input value={editing?.email ?? ''} readOnly disabled className={inputCls} />
          </Field>
          <Field label={t('fields.name')}>
            <input value={editName} onChange={(e) => setEditName(e.target.value)} className={inputCls} />
          </Field>
          <Field label={t('fields.role')}>
            <select value={editRole} onChange={(e) => setEditRole(e.target.value)} className={inputCls}>
              {ROLES.map((r) => (
                <option key={r} value={r}>
                  {t(`users.role.${r}`)}
                </option>
              ))}
            </select>
          </Field>
        </div>
        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('users.editHint')}</p>
      </Modal>

      <FilterBar
        filters={USER_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
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
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                  {rows.length === 0 ? t('common.none') : t('common.noMatch')}
                </td>
              </tr>
            )}
            {filtered.map((u) => {
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
                      <span
                        className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${
                          roleColor[u.role] ?? roleColor.viewer
                        }`}
                      >
                        {t(`users.role.${u.role}`, { defaultValue: u.role })}
                      </span>
                    </td>
                    <td className="px-4 py-2">
                      <StatusBadge value={u.status} />
                    </td>
                    <td className="px-4 py-2 text-slate-500 dark:text-slate-400">
                      {fmtTime(u.created_at)}
                    </td>
                    <td className="px-4 py-2">
                      {isSuper && (
                        <div className="flex flex-wrap gap-1">
                          <ActionBtn onClick={() => openEdit(u)}>{t('common.edit')}</ActionBtn>
                          <ActionBtn onClick={() => toggleStatus(u)} disabled={isSelf}>
                            {u.status === 'active' ? t('common.disable') : t('common.enable')}
                          </ActionBtn>
                          <ActionBtn danger onClick={() => setDeleteId(u.id)} disabled={isSelf}>
                            {t('common.delete')}
                          </ActionBtn>
                        </div>
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

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle')}
        message={t('users.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
