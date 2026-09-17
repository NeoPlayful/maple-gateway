import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import type { AppTemplate, Project, Tenant } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';

// 筛选栏：关键字（项目名/描述）+ 状态 + 租户，纯前端过滤已加载列表。
const PROJECT_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name', 'description'], placeholder: 'projects.filterPh' },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus' },
  { type: 'select', key: 'tenant_id', placeholder: 'common.filterAllTenant' },
];

// 项目：租户 + 模板下的一个部署单元。项目名即项目标识（英文），是数据目录第三段：
// <节点数据目录>/<租户标识>/<模板标识>/<项目标识>。
export default function ProjectsPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<Project[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [templates, setTemplates] = useState<AppTemplate[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<Project | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 表单字段
  const [tenantId, setTenantId] = useState('');
  const [templateId, setTemplateId] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');

  // 实例化弹窗：渲染模板参数并提交创建 Application。
  const [inst, setInst] = useState<{ project: Project; template?: AppTemplate } | null>(null);
  const [instValues, setInstValues] = useState<Record<string, string>>({});
  const [instErr, setInstErr] = useState('');
  const [instBusy, setInstBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      setRows(await api.get<Project[]>('/api/admin/projects?limit=200'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  const loadRefs = useCallback(async () => {
    try {
      const [ts, tps] = await Promise.all([
        api.get<Tenant[]>('/api/admin/tenants?limit=200'),
        api.get<AppTemplate[]>('/api/admin/templates?limit=200'),
      ]);
      setTenants(ts ?? []);
      setTemplates(tps ?? []);
    } catch { /* 引用数据缺失不阻断列表 */ }
  }, []);

  useEffect(() => { load(); loadRefs(); }, [load, loadRefs]);

  const tenantName = (id: string) => tenants.find((x) => x.id === id)?.name ?? '';
  const templateName = (id: string) => templates.find((x) => x.id === id)?.name ?? '';

  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setTenantId('');
    setTemplateId(templates[0]?.id ?? '');
    setName('');
    setDescription('');
    setFormOpen(true);
  };

  const openEdit = (r: Project) => {
    setEditing(r);
    setFormErr('');
    setTenantId(r.tenant_id);
    setTemplateId(r.template_id);
    setName(r.name);
    setDescription(r.description ?? '');
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  const submit = async () => {
    setFormErr('');
    if (!tenantId || !templateId || !name.trim()) {
      setFormErr(t('projects.errFill'));
      return;
    }
    setBusy(true);
    try {
      if (editing) {
        // name/tenant/template 不可改：仅描述可编辑（留空即显式清空）。
        await api.patch(`/api/admin/projects/${editing.id}`, {
          ...(description.trim() ? { description } : { description_clear: true }),
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await api.post('/api/admin/projects', {
          tenant_id: tenantId,
          template_id: templateId,
          name: name.trim(),
          description,
        });
        toast.success(t('common.createSuccess'));
      }
      closeForm();
      await load();
    } catch (e) {
      setFormErr(e instanceof Error ? e.message : editing ? t('common.updateFailed') : t('common.createFailed'));
    } finally {
      setBusy(false);
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await api.delete(`/api/admin/projects/${deleteId}`);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  // 打开实例化弹窗：按模板参数定义预填默认值。
  const openInstantiate = (p: Project) => {
    const tmpl = templates.find((x) => x.id === p.template_id);
    const init: Record<string, string> = {};
    (tmpl?.params ?? []).forEach((pp) => { init[pp.key] = pp.default ?? ''; });
    setInstValues(init);
    setInstErr('');
    setInst({ project: p, template: tmpl });
  };

  const runInstantiate = async () => {
    if (!inst) return;
    setInstErr('');
    setInstBusy(true);
    try {
      await api.post(`/api/admin/projects/${inst.project.id}/instantiate`, { values: instValues });
      toast.success(t('projects.instantiateOk'));
      setInst(null);
      await load();
    } catch (e) {
      setInstErr(e instanceof Error ? e.message : t('projects.instantiateFailed'));
    } finally {
      setInstBusy(false);
    }
  };

  const filtered = useMemo(() => applyFilters(rows, PROJECT_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <PageHeader
        title={t('projects.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            + {t('projects.newProject')}
          </button>
        }
      />

      <Modal
        open={formOpen}
        title={editing ? t('projects.editTitle') : t('projects.createTitle')}
        onClose={closeForm}
        footer={
          <>
            <button
              onClick={closeForm}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={submit}
              disabled={busy}
              className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-50 dark:bg-slate-700 dark:hover:bg-slate-600"
            >
              {busy ? t('common.saving') : editing ? t('common.save') : t('common.create')}
            </button>
          </>
        }
      >
        {formErr && (
          <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
            {formErr}
          </p>
        )}
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          <Field label={t('fields.tenant')}>
            <select value={tenantId} disabled={!!editing} onChange={(e) => setTenantId(e.target.value)} className={inputCls}>
              <option value="">{t('projects.selectTenant')}</option>
              {tenants.map((x) => <option key={x.id} value={x.id}>{x.name} ({x.slug})</option>)}
            </select>
          </Field>
          <Field label={t('projects.template')}>
            <select value={templateId} disabled={!!editing} onChange={(e) => setTemplateId(e.target.value)} className={inputCls}>
              <option value="">{t('projects.selectTemplate')}</option>
              {templates.map((x) => <option key={x.id} value={x.id}>{x.name} ({x.slug})</option>)}
            </select>
          </Field>
          <Field label={t('projects.projectName')}>
            <input
              value={name}
              disabled={!!editing}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('projects.namePh')}
              className={inputCls}
            />
          </Field>
          <Field label={t('fields.description')}>
            <input value={description} onChange={(e) => setDescription(e.target.value)} className={inputCls} />
          </Field>
        </div>
        <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">{t('projects.ownershipHint')}</p>
      </Modal>

      {/* 实例化弹窗：按模板参数定义动态渲染表单。 */}
      <Modal
        open={inst !== null}
        title={t('projects.instantiateTitle')}
        onClose={() => setInst(null)}
        maxWidth="max-w-2xl"
        footer={
          <>
            <button
              onClick={() => setInst(null)}
              className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
            >
              {t('common.cancel')}
            </button>
            <button
              onClick={runInstantiate}
              disabled={instBusy}
              className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-50"
            >
              {instBusy ? t('projects.instantiating') : t('projects.instantiate')}
            </button>
          </>
        }
      >
        {instErr && (
          <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
            {instErr}
          </p>
        )}
        <p className="mb-3 text-xs text-slate-500 dark:text-slate-400">{t('projects.instantiateHint')}</p>
        {(inst?.template?.params ?? []).length === 0 ? (
          <p className="text-sm text-slate-400">{t('projects.noParams')}</p>
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {(inst?.template?.params ?? []).map((p) => (
              <Field key={p.key} label={p.label || p.key}>
                {p.type === 'select' ? (
                  <select
                    value={instValues[p.key] ?? ''}
                    onChange={(e) => setInstValues((v) => ({ ...v, [p.key]: e.target.value }))}
                    className={inputCls}
                  >
                    <option value="">{t('common.select')}</option>
                    {(p.options ?? []).map((o) => <option key={o} value={o}>{o}</option>)}
                  </select>
                ) : p.type === 'bool' ? (
                  <select
                    value={instValues[p.key] ?? ''}
                    onChange={(e) => setInstValues((v) => ({ ...v, [p.key]: e.target.value }))}
                    className={inputCls}
                  >
                    <option value="">-</option>
                    <option value="true">true</option>
                    <option value="false">false</option>
                  </select>
                ) : (
                  <input
                    type={p.type === 'number' ? 'number' : 'text'}
                    value={instValues[p.key] ?? ''}
                    onChange={(e) => setInstValues((v) => ({ ...v, [p.key]: e.target.value }))}
                    placeholder={p.default}
                    className={inputCls}
                  />
                )}
              </Field>
            ))}
          </div>
        )}
      </Modal>

      <FilterBar
        filters={PROJECT_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
        parents={{ '/api/admin/tenants': tenants }}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('projects.projectName')}</th>
              <th className="px-4 py-2">{t('fields.tenant')}</th>
              <th className="px-4 py-2">{t('projects.template')}</th>
              <th className="px-4 py-2">{t('projects.colApp')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                  {rows.length === 0 ? t('projects.none') : t('common.noMatch')}
                </td>
              </tr>
            )}
            {filtered.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400">{tenantName(r.tenant_id) || '-'}</td>
                <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400">{templateName(r.template_id) || '-'}</td>
                <td className="px-4 py-2 font-mono text-xs">{r.application_id ? r.application_id.slice(0, 8) : '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openInstantiate(r)}>{t('projects.instantiate')}</ActionBtn>
                    <ActionBtn onClick={() => openEdit(r)}>{t('common.edit')}</ActionBtn>
                    <ActionBtn danger onClick={() => setDeleteId(r.id)}>{t('common.delete')}</ActionBtn>
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
        message={t('projects.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
