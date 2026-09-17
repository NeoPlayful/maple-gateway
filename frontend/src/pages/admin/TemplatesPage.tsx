import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { api } from '../../lib/client';
import type { AppTemplate, TemplateParam } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';

// 筛选栏：关键字（名称/标识/描述）+ 状态，纯前端过滤已加载列表。
const TEMPLATE_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['name', 'slug', 'description'], placeholder: 'templates.filterPh' },
  { type: 'select', key: 'status', placeholder: 'common.filterAllStatus' },
];

// 一份可直接套用的示例：服务名与镜像由参数渲染，数据目录用 {{data_path}} 引用。
const SAMPLE_SPEC = `services:
  web:
    image: {{image}}
    restart: unless-stopped
    ports:
      - "{{port}}:80"
    volumes:
      - /srv/maple/{{data_path}}/web:/usr/share/nginx/html
`;

const PARAM_TYPES: TemplateParam['type'][] = ['string', 'number', 'bool', 'select'];

// 应用模板管理：带 {{参数键}} 占位符的 Compose 规格 + 参数定义。
// 模板是容器创建的规格来源——用户选模板、填参数即创建一个项目。
export default function TemplatesPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<AppTemplate[]>([]);
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  const [formOpen, setFormOpen] = useState(false);
  const [editing, setEditing] = useState<AppTemplate | null>(null);
  const [busy, setBusy] = useState(false);
  const [formErr, setFormErr] = useState('');
  const [deleteId, setDeleteId] = useState<string | null>(null);

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');
  const [spec, setSpec] = useState('');
  const [params, setParams] = useState<TemplateParam[]>([]);

  const load = useCallback(async () => {
    try {
      setRows(await api.get<AppTemplate[]>('/api/admin/templates?limit=200'));
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => { load(); }, [load]);

  const openCreate = () => {
    setEditing(null);
    setFormErr('');
    setName('');
    setSlug('');
    setDescription('');
    setSpec(SAMPLE_SPEC);
    setParams([]);
    setFormOpen(true);
  };

  const openEdit = (r: AppTemplate) => {
    setEditing(r);
    setFormErr('');
    setName(r.name);
    setSlug(r.slug);
    setDescription(r.description ?? '');
    setSpec(r.spec ?? '');
    setParams(r.params ? r.params.map((p) => ({ ...p })) : []);
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditing(null);
  };

  // 参数定义增删改。
  const addParam = () =>
    setParams((ps) => [...ps, { key: '', label: '', type: 'string' }]);
  const setParam = (i: number, patch: Partial<TemplateParam>) =>
    setParams((ps) => ps.map((p, idx) => (idx === i ? { ...p, ...patch } : p)));
  const removeParam = (i: number) =>
    setParams((ps) => ps.filter((_, idx) => idx !== i));

  const submit = async () => {
    setFormErr('');
    if (!name.trim() || !slug.trim()) {
      setFormErr(t('templates.errFill'));
      return;
    }
    setBusy(true);
    try {
      const cleanParams = params
        .map((p) => ({
          ...p,
          key: p.key.trim(),
          label: p.label.trim() || p.key.trim(),
          options: p.type === 'select'
            ? (p.options ?? []).map((o) => o.trim()).filter(Boolean)
            : undefined,
        }))
        .filter((p) => p.key);
      if (editing) {
        await api.patch(`/api/admin/templates/${editing.id}`, {
          name: name.trim(),
          description,
          spec,
          params: cleanParams,
        });
        toast.success(t('common.updateSuccess'));
      } else {
        await api.post('/api/admin/templates', {
          name: name.trim(),
          slug: slug.trim(),
          description,
          spec,
          params: cleanParams,
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
      await api.delete(`/api/admin/templates/${deleteId}`);
      toast.success(t('common.deleteSuccess'));
      setDeleteId(null);
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const filtered = useMemo(() => applyFilters(rows, TEMPLATE_FILTERS, filterValues), [rows, filterValues]);
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 disabled:opacity-60 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';
  const taCls = `${inputCls} font-mono text-xs`;

  return (
    <div>
      <PageHeader
        title={t('templates.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            + {t('templates.newTemplate')}
          </button>
        }
      />

      <Modal
        open={formOpen}
        title={editing ? t('templates.editTitle') : t('templates.createTitle')}
        onClose={closeForm}
        maxWidth="max-w-3xl"
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
          <Field label={t('fields.name')}>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('templates.namePh')} className={inputCls} />
          </Field>
          <Field label={t('fields.slug')}>
            <input
              value={slug}
              disabled={!!editing}
              onChange={(e) => setSlug(e.target.value)}
              placeholder={t('templates.slugPh')}
              className={inputCls}
            />
          </Field>
        </div>
        <div className="mt-3">
          <Field label={t('fields.description')}>
            <input value={description} onChange={(e) => setDescription(e.target.value)} className={inputCls} />
          </Field>
        </div>
        <div className="mt-3">
          <Field label={t('templates.specLabel')}>
            <textarea value={spec} onChange={(e) => setSpec(e.target.value)} rows={10} spellCheck={false} className={taCls} />
          </Field>
          <p className="mt-1 text-xs text-slate-400">{t('templates.specHint')}</p>
        </div>

        {/* 参数定义：每个键对应规格中的 {{键}}；data_path 为内置可用的数据目录占位符。 */}
        <div className="mt-4 border-t border-slate-200 pt-3 dark:border-slate-700">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{t('templates.params')}</span>
            <button
              type="button"
              onClick={addParam}
              className="rounded border border-slate-300 px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-700"
            >
              + {t('templates.addParam')}
            </button>
          </div>
          <p className="mb-2 text-xs text-slate-500 dark:text-slate-400">{t('templates.paramsHint')}</p>
          {params.length === 0 && (
            <p className="text-xs text-slate-400 dark:text-slate-500">{t('templates.noParams')}</p>
          )}
          <div className="space-y-2">
            {params.map((p, i) => (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <input
                  value={p.key}
                  onChange={(e) => setParam(i, { key: e.target.value })}
                  placeholder={t('templates.paramKeyPh')}
                  className={`${inputCls} min-w-[8rem] flex-1`}
                />
                <input
                  value={p.label}
                  onChange={(e) => setParam(i, { label: e.target.value })}
                  placeholder={t('templates.paramLabelPh')}
                  className={`${inputCls} min-w-[8rem] flex-1`}
                />
                <select
                  value={p.type}
                  onChange={(e) => setParam(i, { type: e.target.value as TemplateParam['type'] })}
                  className={`${inputCls} w-28`}
                >
                  {PARAM_TYPES.map((ty) => <option key={ty} value={ty}>{ty}</option>)}
                </select>
                {p.type === 'select' ? (
                  <input
                    value={(p.options ?? []).join(',')}
                    onChange={(e) => setParam(i, { options: e.target.value.split(',') })}
                    placeholder={t('templates.paramOptionsPh')}
                    className={`${inputCls} min-w-[10rem] flex-1`}
                  />
                ) : (
                  <input
                    value={p.default ?? ''}
                    onChange={(e) => setParam(i, { default: e.target.value })}
                    placeholder={t('templates.paramDefaultPh')}
                    className={`${inputCls} min-w-[8rem] flex-1`}
                  />
                )}
                <label className="flex items-center gap-1 text-xs text-slate-600 dark:text-slate-300">
                  <input type="checkbox" checked={!!p.required} onChange={(e) => setParam(i, { required: e.target.checked })} />
                  {t('templates.paramRequired')}
                </label>
                <button
                  type="button"
                  onClick={() => removeParam(i)}
                  className="rounded px-2 py-1 text-xs text-rose-600 hover:bg-rose-50 dark:text-rose-400 dark:hover:bg-rose-900/30"
                >
                  {t('common.delete')}
                </button>
              </div>
            ))}
          </div>
        </div>
      </Modal>

      <FilterBar
        filters={TEMPLATE_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('fields.slug')}</th>
              <th className="px-4 py-2">{t('templates.colParams')}</th>
              <th className="px-4 py-2">{t('fields.description')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                  {rows.length === 0 ? t('templates.none') : t('common.noMatch')}
                </td>
              </tr>
            )}
            {filtered.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2 font-mono text-xs">{r.slug}</td>
                <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400">{(r.params ?? []).length || '-'}</td>
                <td className="px-4 py-2 text-xs text-slate-500 dark:text-slate-400">{r.description || '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
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
        message={t('templates.confirmDelete')}
        onConfirm={doDelete}
        onCancel={() => setDeleteId(null)}
      />
    </div>
  );
}
