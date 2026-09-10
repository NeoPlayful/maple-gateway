// 配置驱动的通用资源管理页：列表 + 创建 + 启用/禁用 + 删除。
// 适用于结构规整的后端 CRUD 资源（tenants/services/domains/deployments/nodes）。
import { Fragment, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { ChevronDownIcon, ChevronRightIcon } from '@heroicons/react/24/outline';
import { list, create, update, remove, statusAction } from '../lib/modules';
import { StatusBadge } from './admin/StatusBadge';
import { ActionBtn } from './admin/ActionBtn';
import { Modal } from './admin/Modal';
import { ConfirmDialog } from './admin/ConfirmDialog';

export interface FieldDef {
  key: string;
  label: string; // i18n key 或原文案
  type?: 'text' | 'number' | 'select' | 'textarea';
  required?: boolean;
  // select 选项：静态数组或从父资源拉取。
  options?: { value: string; label: string }[];
  loadOptions?: { labelKey: string; valueKey: string; path: string };
  placeholder?: string;
  // 编辑模式下值为空时显式提交 null（用于"清空关联"，如 service_id）。
  clearOnEmpty?: boolean;
}

export interface PageDef {
  title: string; // i18n key
  path: string; // /api/admin/<resource>
  listName?: string; // 列表主键 key
  columns: { key: string; label: string; badge?: boolean; render?: (r: any) => ReactNode }[];
  createFields: FieldDef[];
  editFields?: FieldDef[]; // 声明后行内显示"编辑"，PATCH 部分更新
  renderDetail?: (row: any) => ReactNode; // 声明后行首显示展开箭头，展开渲染该行详情
  statusActions?: ('enable' | 'disable')[]; // 提供 enable/disable 动作
  extraActions?: { label: string; act?: string; onClick?: (id: string) => void }[];
}

export default function CrudPage({ def }: { def: PageDef }) {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<any[]>([]);
  const [parents, setParents] = useState<Record<string, any[]>>({});
  // 弹窗态：null=关闭；否则为当前模式（新建/编辑）及表单值、编辑行 id。
  const [modal, setModal] = useState<{
    mode: 'create' | 'edit';
    id: string | null;
    values: Record<string, string>;
  } | null>(null);
  // 待删除行 id：非空时显示确认弹窗。
  const [deleteId, setDeleteId] = useState<string | null>(null);
  // 当前展开详情的行 id（手风琴式，同一时刻最多展开一行）。
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      setRows(await list(def.path));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed', '加载失败'));
    }
  }, [def.path, t]);

  const loadParents = useCallback(async () => {
    const out: Record<string, any[]> = {};
    const fields = [...def.createFields, ...(def.editFields ?? [])];
    for (const f of fields) {
      if (f.loadOptions && !out[f.loadOptions.path]) {
        try {
          out[f.loadOptions.path] = await list(f.loadOptions.path);
        } catch {
          out[f.loadOptions.path] = [];
        }
      }
    }
    setParents(out);
  }, [def.createFields, def.editFields]);

  useEffect(() => {
    load();
    loadParents();
  }, [load, loadParents]);

  const opt = (f: FieldDef) =>
    f.loadOptions
      ? (parents[f.loadOptions.path] ?? []).map((p) => ({
          value: String(p[f.loadOptions!.valueKey]),
          label: String(p[f.loadOptions!.labelKey]),
        }))
      : (f.options ?? []);

  // 打开新建弹窗：清空表单。
  const openCreate = () => {
    setModal({ mode: 'create', id: null, values: {} });
  };

  // 打开编辑弹窗：用当前行值回填表单。
  const openEdit = (row: any) => {
    const init: Record<string, string> = {};
    for (const f of def.editFields ?? []) {
      const v = row[f.key];
      init[f.key] = v === null || v === undefined ? '' : String(v);
    }
    setModal({ mode: 'edit', id: row.id, values: init });
  };

  const closeModal = () => {
    setModal(null);
  };

  // 更新弹窗内某个字段值。
  const setField = (key: string, value: string) =>
    setModal((m) => (m ? { ...m, values: { ...m.values, [key]: value } } : m));

  const runAction = async (id: string, act: string) => {
    try {
      await statusAction(def.path, id, act);
      toast.success(t('common.operateSuccess', '操作成功'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed', '操作失败'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await remove(def.path, deleteId);
      toast.success(t('common.deleteSuccess', '已删除'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed', '删除失败'));
    }
  };

  // 由字段配置与受控表单构造提交体；必填校验失败返回 error 文案。
  const collectBody = (
    fields: FieldDef[],
    values: Record<string, string>,
    mode: 'create' | 'edit',
  ): { body: Record<string, unknown> } | { error: string } => {
    const body: Record<string, unknown> = {};
    for (const f of fields) {
      const v = values[f.key];
      if (f.required && !v) {
        return {
          error: t('crud.requiredField', { defaultValue: '请填写{{field}}', field: t(f.label) }),
        };
      }
      if (v === '' || v === undefined) {
        // 编辑模式："清空关联"字段显式发 <key>_clear=true；其余留空=不改该字段（部分更新）。
        if (mode === 'edit' && f.clearOnEmpty) body[`${f.key}_clear`] = true;
        continue;
      }
      body[f.key] = f.type === 'number' ? Number(v) : v;
    }
    return { body };
  };

  // 提交弹窗表单：按模式走 create / update，成功后关闭并刷新。
  const submitModal = async () => {
    if (!modal) return;
    const fields = modal.mode === 'create' ? def.createFields : def.editFields ?? [];
    const collected = collectBody(fields, modal.values, modal.mode);
    if ('error' in collected) {
      toast.error(collected.error);
      return;
    }
    try {
      if (modal.mode === 'create') {
        await create(def.path, collected.body);
        toast.success(t('common.createSuccess', '创建成功'));
      } else if (modal.id) {
        await update(def.path, modal.id, collected.body);
        toast.success(t('common.updateSuccess', '已保存'));
      }
      closeModal();
      await load();
    } catch (e) {
      // 保留弹窗，便于修正后重提。
      const fallback = modal.mode === 'create'
        ? t('common.createFailed', '创建失败')
        : t('common.updateFailed', '保存失败');
      toast.error(e instanceof Error ? e.message : fallback);
    }
  };

  // 状态分组：enabled/active 视为“可禁用”，disabled/suspended 视为“可启用”。
  const activeStates = ['active', 'enabled', 'online', 'running', 'healthy'];
  const inactiveStates = ['disabled', 'suspended', 'offline', 'maintenance', 'paused'];
  const isActive = (s: string) => activeStates.includes(s);
  const isInactive = (s: string) => inactiveStates.includes(s);

  const selectable = useMemo(
    () => def.statusActions && def.statusActions.length > 0,
    [def.statusActions],
  );

  const fieldCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';

  // 弹窗表单字段控件（数据源为 modal.values / setField）。
  const renderField = (f: FieldDef) => {
    const opts = opt(f);
    const placeholder = f.placeholder ? t(f.placeholder) : undefined;
    const value = modal?.values[f.key] ?? '';
    if (f.type === 'select' || f.loadOptions) {
      return (
        <div key={f.key}>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">
            {t(f.label)}
          </label>
          <select
            value={value}
            onChange={(e) => setField(f.key, e.target.value)}
            className={fieldCls}
          >
            <option value="">{t('common.select', '选择…')}</option>
            {opts.map((o) => (
              <option key={o.value} value={o.value}>
                {t(o.label, { defaultValue: o.label })}
              </option>
            ))}
          </select>
        </div>
      );
    }
    if (f.type === 'textarea') {
      return (
        <div key={f.key}>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">
            {t(f.label)}
          </label>
          <textarea
            value={value}
            onChange={(e) => setField(f.key, e.target.value)}
            className={fieldCls}
          />
        </div>
      );
    }
    return (
      <div key={f.key}>
        <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">
          {t(f.label)}
        </label>
        <input
          type={f.type === 'number' ? 'number' : 'text'}
          placeholder={placeholder}
          value={value}
          onChange={(e) => setField(f.key, e.target.value)}
          className={fieldCls}
        />
      </div>
    );
  };

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t(def.title)}</h1>
        <button
          onClick={openCreate}
          className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
        >
          {`+ ${t('common.new', '新建')}${t(def.title)}`}
        </button>
      </div>
      <Modal
        open={modal !== null}
        title={
          modal?.mode === 'edit'
            ? `${t('common.edit', '编辑')}${
                rows.find((r) => r.id === modal.id)?.[def.columns[0].key] ?? ''
              }`
            : `${t('common.new', '新建')}${t(def.title)}`
        }
        onClose={closeModal}
        footer={
          <>
            <button
              onClick={closeModal}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel', '取消')}
            </button>
            <button
              onClick={submitModal}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:text-white dark:hover:bg-slate-600"
            >
              {modal?.mode === 'edit'
                ? t('common.save', '保存')
                : t('common.create', '创建')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
          {(modal?.mode === 'create' ? def.createFields : def.editFields ?? []).map((f) =>
            renderField(f),
          )}
        </div>
      </Modal>

      <ConfirmDialog
        open={deleteId !== null}
        title={t('common.confirmTitle', '确认操作')}
        message={t('common.confirmDelete', '确认删除？')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              {def.columns.map((c) => (
                <th key={c.key} className="px-4 py-2">
                  {t(c.label)}
                </th>
              ))}
              <th className="px-4 py-2">{t('common.action', '操作')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td
                  colSpan={def.columns.length + 1}
                  className="px-4 py-6 text-center text-slate-400 dark:text-slate-500"
                >
                  {t('common.none', '暂无数据')}
                </td>
              </tr>
            )}
            {rows.map((r) => (
              <Fragment key={r.id}>
                <tr
                  onClick={
                    def.renderDetail
                      ? () => {
                          // 选中文字时不触发展开（在行内拖选复制时不希望误切换）。
                          if (window.getSelection()?.toString()) return;
                          setExpandedId((cur) => (cur === r.id ? null : r.id));
                        }
                      : undefined
                  }
                  className={`border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40${
                    def.renderDetail ? ' cursor-pointer' : ''
                  }`}
                >
                  {def.columns.map((c, ci) => (
                    <td key={c.key} className="px-4 py-2">
                      <span className="inline-flex items-center gap-1">
                        {ci === 0 && def.renderDetail && (
                          <button
                            type="button"
                            onClick={(e) => {
                              e.stopPropagation();
                              setExpandedId((cur) => (cur === r.id ? null : r.id));
                            }}
                            aria-label={
                              expandedId === r.id
                                ? t('common.collapse', '收起')
                                : t('common.expand', '展开')
                            }
                            className="shrink-0 text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
                          >
                            {expandedId === r.id ? (
                              <ChevronDownIcon className="h-4 w-4" />
                            ) : (
                              <ChevronRightIcon className="h-4 w-4" />
                            )}
                          </button>
                        )}
                        {c.badge ? (
                          <StatusBadge value={String(r[c.key])} />
                        ) : c.render ? (
                          c.render(r)
                        ) : (
                          String(r[c.key] ?? '')
                        )}
                      </span>
                    </td>
                  ))}
                  <td className="px-4 py-2" onClick={(e) => e.stopPropagation()}>
                    <div className="flex flex-wrap gap-1">
                    {def.editFields && def.editFields.length > 0 && (
                      <ActionBtn onClick={() => openEdit(r)}>{t('common.edit', '编辑')}</ActionBtn>
                    )}
                    {def.extraActions?.map((ea) =>
                      ea.act ? (
                        <ActionBtn key={ea.label} onClick={() => runAction(r.id, ea.act!)}>
                          {t(ea.label)}
                        </ActionBtn>
                      ) : (
                        <ActionBtn key={ea.label} onClick={() => ea.onClick!(r.id)}>
                          {t(ea.label)}
                        </ActionBtn>
                      ),
                    )}
                    {selectable &&
                      def.statusActions!.map((sa) =>
                        sa === 'enable'
                          ? isInactive(r.status)
                            ? (
                                <ActionBtn key={sa} onClick={() => runAction(r.id, sa)}>
                                  {t('common.enable', '启用')}
                                </ActionBtn>
                              )
                            : null
                          : isActive(r.status)
                            ? (
                                <ActionBtn key={sa} onClick={() => runAction(r.id, sa)}>
                                  {t('common.disable', '禁用')}
                                </ActionBtn>
                              )
                            : null,
                      )}
                    <ActionBtn danger onClick={() => setDeleteId(r.id)}>
                      {t('common.delete', '删除')}
                    </ActionBtn>
                  </div>
                </td>
                </tr>
                {def.renderDetail && expandedId === r.id && (
                  <tr className="border-b border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40">
                    <td colSpan={def.columns.length + 1} className="px-4 py-3">
                      {def.renderDetail(r)}
                    </td>
                  </tr>
                )}
              </Fragment>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
