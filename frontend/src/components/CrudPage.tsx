// 配置驱动的通用资源管理页：列表 + 创建 + 启用/禁用 + 删除。
// 适用于结构规整的后端 CRUD 资源（tenants/services/domains/deployments/nodes）。
import { useCallback, useEffect, useMemo, useState } from 'react';
import { list, create, remove, statusAction } from '../lib/modules';
import { StatusBadge, ActionBtn } from '../components/ui';

export interface FieldDef {
  key: string;
  label: string;
  type?: 'text' | 'number' | 'select' | 'textarea';
  required?: boolean;
  // select 选项：静态数组或从父资源拉取。
  options?: { value: string; label: string }[];
  loadOptions?: { labelKey: string; valueKey: string; path: string };
  placeholder?: string;
}

export interface PageDef {
  title: string;
  path: string; // /api/admin/<resource>
  listName?: string; // 列表主键 key
  columns: { key: string; label: string; badge?: boolean; render?: (r: any) => string }[];
  createFields: FieldDef[];
  statusActions?: ('enable' | 'disable')[]; // 提供 enable/disable 动作
  extraActions?: { label: string; act?: string; onClick?: (id: string) => void }[];
}

export default function CrudPage({ def }: { def: PageDef }) {
  const [rows, setRows] = useState<any[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [parents, setParents] = useState<Record<string, any[]>>({});
  const [form, setForm] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      setRows(await list(def.path));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, [def.path]);

  const loadParents = useCallback(async () => {
    const out: Record<string, any[]> = {};
    for (const f of def.createFields) {
      if (f.loadOptions && !out[f.loadOptions.path]) {
        try {
          out[f.loadOptions.path] = await list(f.loadOptions.path);
        } catch {
          out[f.loadOptions.path] = [];
        }
      }
    }
    setParents(out);
  }, [def.createFields]);

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

  const runAction = async (id: string, act: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction(def.path, id, act);
      setMsg('操作成功');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '操作失败');
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm('确认删除？')) return;
    setErr('');
    setMsg('');
    try {
      await remove(def.path, id);
      setMsg('已删除');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '删除失败');
    }
  };

  const submit = async () => {
    setErr('');
    setMsg('');
    const body: Record<string, unknown> = {};
    for (const f of def.createFields) {
      const v = form[f.key];
      if (f.required && !v) {
        setErr(`请填写${f.label}`);
        return;
      }
      if (v === '' || v === undefined) continue;
      body[f.key] = f.type === 'number' ? Number(v) : v;
    }
    try {
      await create(def.path, body);
      setMsg('创建成功');
      setOpen(false);
      setForm({});
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '创建失败');
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

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">{def.title}</h1>
        <button
          onClick={() => setOpen((v) => !v)}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700"
        >
          {open ? '收起' : `+ 新建${def.title}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl bg-white p-5 shadow-sm">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
            {def.createFields.map((f) => (
              <div key={f.key}>
                <label className="mb-1 block text-xs text-slate-500">{f.label}</label>
                {f.type === 'select' || f.loadOptions ? (
                  <select
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border px-2 py-1.5 text-sm"
                  >
                    <option value="">选择…</option>
                    {opt(f).map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                ) : f.type === 'textarea' ? (
                  <textarea
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border px-2 py-1.5 text-sm"
                  />
                ) : (
                  <input
                    type={f.type === 'number' ? 'number' : 'text'}
                    value={form[f.key] ?? ''}
                    onChange={(e) => setForm((m) => ({ ...m, [f.key]: e.target.value }))}
                    className="w-full rounded border px-2 py-1.5 text-sm"
                  />
                )}
              </div>
            ))}
          </div>
          <button
            onClick={submit}
            className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700"
          >
            创建
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
            <tr>
              {def.columns.map((c) => (
                <th key={c.key} className="px-4 py-2">
                  {c.label}
                </th>
              ))}
              <th className="px-4 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={def.columns.length + 1} className="px-4 py-6 text-center text-slate-400">
                  暂无数据
                </td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b hover:bg-slate-50">
                {def.columns.map((c) => (
                  <td key={c.key} className="px-4 py-2">
                    {c.badge ? <StatusBadge value={String(r[c.key])} /> : c.render ? c.render(r) : String(r[c.key] ?? '')}
                  </td>
                ))}
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {def.extraActions?.map((ea) =>
                      ea.act ? (
                        <ActionBtn key={ea.label} onClick={() => runAction(r.id, ea.act!)}>
                          {ea.label}
                        </ActionBtn>
                      ) : (
                        <ActionBtn key={ea.label} onClick={() => ea.onClick!(r.id)}>
                          {ea.label}
                        </ActionBtn>
                      ),
                    )}
                    {selectable &&
                      def.statusActions!.map((sa) =>
                        sa === 'enable'
                          ? isInactive(r.status)
                            ? <ActionBtn key={sa} onClick={() => runAction(r.id, sa)}>启用</ActionBtn>
                            : null
                          : isActive(r.status)
                            ? <ActionBtn key={sa} onClick={() => runAction(r.id, sa)}>禁用</ActionBtn>
                            : null,
                      )}
                    <ActionBtn danger onClick={() => doDelete(r.id)}>
                      删除
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
