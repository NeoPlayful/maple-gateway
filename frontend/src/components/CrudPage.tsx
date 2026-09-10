// 配置驱动的通用资源管理页：列表 + 创建 + 启用/禁用 + 删除。
// 适用于结构规整的后端 CRUD 资源（tenants/services/domains/deployments/nodes）。
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { list, create, update, remove, statusAction } from '../lib/modules';
import { StatusBadge, ActionBtn } from '../components/ui';

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
  columns: { key: string; label: string; badge?: boolean; render?: (r: any) => string }[];
  createFields: FieldDef[];
  editFields?: FieldDef[]; // 声明后行内显示"编辑"，PATCH 部分更新
  statusActions?: ('enable' | 'disable')[]; // 提供 enable/disable 动作
  extraActions?: { label: string; act?: string; onClick?: (id: string) => void }[];
}

export default function CrudPage({ def }: { def: PageDef }) {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<any[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [parents, setParents] = useState<Record<string, any[]>>({});
  const [form, setForm] = useState<Record<string, string>>({});
  // 编辑态：editingId 为当前编辑行 id，editing 为其回填表单值。
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editing, setEditing] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      setRows(await list(def.path));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed', '加载失败'));
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

  // 打开编辑：用当前行值回填表单；同时收起新建表单。
  const openEdit = (row: any) => {
    setOpen(false);
    const init: Record<string, string> = {};
    for (const f of def.editFields ?? []) {
      const v = row[f.key];
      init[f.key] = v === null || v === undefined ? '' : String(v);
    }
    setEditing(init);
    setEditingId(row.id);
  };

  const cancelEdit = () => {
    setEditingId(null);
    setEditing({});
  };

  const runAction = async (id: string, act: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction(def.path, id, act);
      setMsg(t('common.operateSuccess', '操作成功'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed', '操作失败'));
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm(t('common.confirmDelete', '确认删除？'))) return;
    setErr('');
    setMsg('');
    try {
      await remove(def.path, id);
      setMsg(t('common.deleteSuccess', '已删除'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.deleteFailed', '删除失败'));
    }
  };

  // 由字段配置与受控表单构造提交体；必填校验失败返回 null。
  const collectBody = (
    fields: FieldDef[],
    values: Record<string, string>,
    mode: 'create' | 'edit',
  ): Record<string, unknown> | null => {
    const body: Record<string, unknown> = {};
    for (const f of fields) {
      const v = values[f.key];
      if (f.required && !v) {
        setErr(t('crud.requiredField', { defaultValue: '请填写{{field}}', field: t(f.label) }));
        return null;
      }
      if (v === '' || v === undefined) {
        // 编辑模式："清空关联"字段显式发 <key>_clear=true；其余留空=不改该字段（部分更新）。
        if (mode === 'edit' && f.clearOnEmpty) body[`${f.key}_clear`] = true;
        continue;
      }
      body[f.key] = f.type === 'number' ? Number(v) : v;
    }
    return body;
  };

  const submit = async () => {
    setErr('');
    setMsg('');
    const body = collectBody(def.createFields, form, 'create');
    if (!body) return;
    try {
      await create(def.path, body);
      setMsg(t('common.createSuccess', '创建成功'));
      setOpen(false);
      setForm({});
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed', '创建失败'));
    }
  };

  const submitEdit = async () => {
    if (!editingId) return;
    setErr('');
    setMsg('');
    const body = collectBody(def.editFields ?? [], editing, 'edit');
    if (!body) return;
    try {
      await update(def.path, editingId, body);
      setMsg(t('common.updateSuccess', '已保存'));
      cancelEdit();
      await load();
    } catch (e) {
      // 保留编辑面板，便于修正后重提。
      setErr(e instanceof Error ? e.message : t('common.updateFailed', '保存失败'));
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

  // 编辑表单字段控件（数据源为 editing / setEditing）。
  const renderEditField = (f: FieldDef) => {
    const opts = opt(f);
    const placeholder = f.placeholder ? t(f.placeholder) : undefined;
    if (f.type === 'select' || f.loadOptions) {
      return (
        <div key={f.key}>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">
            {t(f.label)}
          </label>
          <select
            value={editing[f.key] ?? ''}
            onChange={(e) => setEditing((m) => ({ ...m, [f.key]: e.target.value }))}
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
            value={editing[f.key] ?? ''}
            onChange={(e) => setEditing((m) => ({ ...m, [f.key]: e.target.value }))}
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
          value={editing[f.key] ?? ''}
          onChange={(e) => setEditing((m) => ({ ...m, [f.key]: e.target.value }))}
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
          onClick={() => {
            setOpen((v) => !v);
            cancelEdit();
          }}
          className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
        >
          {open ? t('common.collapse', '收起') : `+ ${t('common.new', '新建')}${t(def.title)}`}
        </button>
      </div>
      {msg && (
        <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">
          {msg}
        </p>
      )}
      {err && (
        <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
          {err}
        </p>
      )}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            {def.createFields.map((f) => (
              <div key={f.key}>
                <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">
                  {t(f.label)}
                </label>
                {f.type === 'select' || f.loadOptions ? (
                  <select
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200"
                  >
                    <option value="">{t('common.select', '选择…')}</option>
                    {opt(f).map((o) => (
                      <option key={o.value} value={o.value}>
                        {t(o.label, { defaultValue: o.label })}
                      </option>
                    ))}
                  </select>
                ) : f.type === 'textarea' ? (
                  <textarea
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200"
                  />
                ) : (
                  <input
                    type={f.type === 'number' ? 'number' : 'text'}
                    placeholder={f.placeholder ? t(f.placeholder) : undefined}
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500"
                  />
                )}
              </div>
            ))}
          </div>
          <button
            onClick={submit}
            className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:text-white dark:hover:bg-slate-600"
          >
            {t('common.create', '创建')}
          </button>
        </div>
      )}

      {editingId && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <h2 className="mb-3 text-sm font-semibold text-slate-700 dark:text-slate-200">
            {t('common.edit', '编辑')}
            {rows.find((r) => r.id === editingId)?.[def.columns[0].key] ?? ''}
          </h2>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            {(def.editFields ?? []).map((f) => renderEditField(f))}
          </div>
          <div className="mt-4 flex gap-2">
            <button
              onClick={submitEdit}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:text-white dark:hover:bg-slate-600"
            >
              {t('common.save', '保存')}
            </button>
            <button
              onClick={cancelEdit}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel', '取消')}
            </button>
          </div>
        </div>
      )}

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
              <tr
                key={r.id}
                className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40"
              >
                {def.columns.map((c) => (
                  <td key={c.key} className="px-4 py-2">
                    {c.badge ? (
                      <StatusBadge value={String(r[c.key])} />
                    ) : c.render ? (
                      c.render(r)
                    ) : (
                      String(r[c.key] ?? '')
                    )}
                  </td>
                ))}
                <td className="px-4 py-2">
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
                    <ActionBtn danger onClick={() => doDelete(r.id)}>
                      {t('common.delete', '删除')}
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
