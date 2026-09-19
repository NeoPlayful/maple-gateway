import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import type { Deployment, Mount, Service, VersionWithOwner } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { FilterBar, applyFilters, type FilterDef } from '../../components/admin/FilterBar';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

// 筛选栏：关键字（版本/镜像）+ 服务 + 状态，纯前端过滤已加载的全部版本。
const VERSION_FILTERS: FilterDef[] = [
  { type: 'keyword', key: 'keyword', keys: ['version', 'image'], placeholder: 'versions.filterPh' },
  { type: 'select', key: 'service_id', placeholder: 'versions.filterAllServices', loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' } },
  { type: 'select', key: 'status', placeholder: 'versions.filterAllStatus', labelPrefix: 'status' },
];

// 版本管理：全局平铺所有服务/部署下的"可执行规格"（镜像 / 副本数 / 端口 / 环境变量 / 调度约束）。
// 版本规格即 Container Manager 的部署意图来源——保存后由 Gateway Leader 推给 CM。
export default function VersionsPage() {
  const { t } = useTranslation('admin');
  const [services, setServices] = useState<Service[]>([]);
  const [rows, setRows] = useState<VersionWithOwner[]>([]);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<VersionWithOwner | null>(null);
  const [deleteRow, setDeleteRow] = useState<VersionWithOwner | null>(null);
  // 新建弹窗内联动加载的部署（随所选服务变化）。
  const [formDeploys, setFormDeploys] = useState<Deployment[]>([]);

  // 列表筛选：关键字 / 服务 / 状态，纯前端过滤已加载的全部版本。
  const [filterValues, setFilterValues] = useState<Record<string, string>>({});

  // 表单态（字符串保存原始输入，提交时转换）。svcId/depId 仅新建时用于定位所属部署。
  const [form, setForm] = useState({
    svcId: '',
    depId: '',
    version: '',
    image: '',
    replicas: '1',
    port: '',
    weight: '100',
    status: 'stable',
    health_path: '',
    env: '',
    node_selector: '',
    mounts: [] as Mount[],
  });

  const loadServices = useCallback(async () => {
    try {
      setServices(await api.get<Service[]>('/api/admin/services?limit=200'));
    } catch { /* ignore */ }
  }, []);

  const loadVersions = useCallback(async () => {
    try {
      setRows(await api.get<VersionWithOwner[]>('/api/admin/versions?limit=200'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
      setRows([]);
    }
  }, [t]);

  useEffect(() => { loadServices(); loadVersions(); }, [loadServices, loadVersions]);

  // 新建弹窗内选服务 → 联动加载其部署。
  const loadFormDeploys = async (serviceId: string) => {
    setForm((f) => ({ ...f, svcId: serviceId, depId: '' }));
    if (!serviceId) { setFormDeploys([]); return; }
    try {
      setFormDeploys(await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=200`));
    } catch { setFormDeploys([]); }
  };

  const openCreate = () => {
    setEditing(null);
    setFormDeploys([]);
    setForm({
      svcId: '', depId: '', version: '', image: '', replicas: '1', port: '',
      weight: '100', status: 'stable', health_path: '', env: '', node_selector: '', mounts: [],
    });
    setModalOpen(true);
  };

  const openEdit = (v: VersionWithOwner) => {
    setEditing(v);
    setFormDeploys([]);
    setForm({
      svcId: v.service_id,
      depId: v.deployment_id,
      version: v.version,
      image: v.image ?? '',
      replicas: String(v.replicas ?? 1),
      port: v.port ? String(v.port) : '',
      weight: String(v.weight ?? 100),
      status: v.status,
      health_path: v.health_path ?? '',
      env: v.env ? Object.entries(v.env).map(([k, val]) => `${k}=${val}`).join('\n') : '',
      node_selector: v.node_selector ? Object.entries(v.node_selector).map(([k, val]) => `${k}=${val}`).join('\n') : '',
      mounts: v.mounts ? v.mounts.map((m) => ({ ...m })) : [],
    });
    setModalOpen(true);
  };

  // 挂载项增删改：path 相对节点数据根，target 为容器内绝对路径。
  const addMount = () => setForm((f) => ({ ...f, mounts: [...f.mounts, { path: '', target: '' }] }));
  const setMount = (i: number, patch: Partial<Mount>) =>
    setForm((f) => ({ ...f, mounts: f.mounts.map((m, idx) => (idx === i ? { ...m, ...patch } : m)) }));
  const removeMount = (i: number) =>
    setForm((f) => ({ ...f, mounts: f.mounts.filter((_, idx) => idx !== i) }));

  // parseKV 把 "k=v" 多行文本解析为对象。
  const parseKV = (s: string): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const line of s.split('\n')) {
      const idx = line.indexOf('=');
      if (idx <= 0) continue;
      const k = line.slice(0, idx).trim();
      const v = line.slice(idx + 1).trim();
      if (k) out[k] = v;
    }
    return out;
  };

  const submit = async () => {
    setErr('');
    try {
      const env = parseKV(form.env);
      const nodeSelector = parseKV(form.node_selector);
      // 丢弃两字段皆空的行后整体提交（后端按当前表单整体替换）。
      const mounts = form.mounts
        .map((m) => ({ path: m.path.trim(), target: m.target.trim(), read_only: !!m.read_only }))
        .filter((m) => m.path && m.target);
      const body = {
        version: form.version,
        image: form.image,
        replicas: Number(form.replicas) || 1,
        port: form.port ? Number(form.port) : 0,
        weight: Number(form.weight) || 0,
        status: form.status,
        health_path: form.health_path,
        env,
        node_selector: nodeSelector,
        mounts,
      };
      if (editing) {
        await api.patch(`/api/admin/versions/${editing.id}`, body);
        setMsg(t('versions.updateHint'));
      } else {
        await api.post(`/api/admin/deployments/${form.depId}/versions`, body);
        setMsg(t('versions.createHint'));
      }
      setModalOpen(false);
      await loadVersions();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const setDefault = async (v: VersionWithOwner) => {
    setErr('');
    try {
      await api.post(`/api/admin/versions/${v.id}/default`, { deployment_id: v.deployment_id });
      setMsg(t('versions.defaultHint'));
      await loadVersions();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteRow) return;
    try {
      await api.delete(`/api/admin/versions/${deleteRow.id}`);
      setMsg(t('versions.deleteHint'));
      setDeleteRow(null);
      await loadVersions();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  // 新建时需先选服务与部署；编辑时仅需版本与镜像。
  const canSubmit = useMemo(
    () => !!form.version && !!form.image && (editing ? true : !!form.depId),
    [form.version, form.image, form.depId, editing],
  );

  const filtered = useMemo(
    () => applyFilters(rows, VERSION_FILTERS, filterValues),
    [rows, filterValues],
  );
  const setFilter = (key: string, value: string) =>
    setFilterValues((cur) => ({ ...cur, [key]: value }));
  const resetFilter = () => setFilterValues({});

  // fieldCls 只含盒子样式（不含宽度）。弹窗输入框需要占满一行用 inputCls。
  const fieldCls =
    'rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';
  const inputCls = `${fieldCls} w-full`;

  return (
    <div>
      <PageHeader
        title={t('versions.title')}
        right={
          <button
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover"
          >
            + {t('versions.newVersion')}
          </button>
        }
      />
      {msg && <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      <FilterBar
        filters={VERSION_FILTERS}
        values={filterValues}
        onChange={setFilter}
        onReset={resetFilter}
        rows={rows}
        parents={{ '/api/admin/services': services }}
      />

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.service')}</th>
              <th className="px-4 py-2">{t('fields.deploymentName')}</th>
              <th className="px-4 py-2">{t('fields.version')}</th>
              <th className="px-4 py-2">{t('fields.image')}</th>
              <th className="px-4 py-2">{t('fields.replicas')}</th>
              <th className="px-4 py-2">{t('fields.port')}</th>
              <th className="px-4 py-2">{t('fields.weight')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('fields.healthPath')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr><td colSpan={10} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">
                {rows.length === 0 ? t('versions.none') : t('versions.noMatch')}
              </td></tr>
            )}
            {filtered.map((v) => (
              <tr key={v.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 text-slate-600 dark:text-slate-300">{v.service_name || shortId(v.service_id)}</td>
                <td className="px-4 py-2 text-slate-600 dark:text-slate-300">{v.deployment_name || shortId(v.deployment_id)}</td>
                <td className="px-4 py-2">{v.version}</td>
                <td className="px-4 py-2 font-mono text-xs">{v.image || '-'}</td>
                <td className="px-4 py-2">{v.replicas ?? 1}</td>
                <td className="px-4 py-2">{v.port || '-'}</td>
                <td className="px-4 py-2">{v.weight}</td>
                <td className="px-4 py-2"><StatusBadge value={v.status} /></td>
                <td className="px-4 py-2 font-mono text-xs">{v.health_path || '-'}</td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    <ActionBtn onClick={() => openEdit(v)}>{t('common.edit')}</ActionBtn>
                    {v.status !== 'stable' && <ActionBtn onClick={() => setDefault(v)}>{t('versions.setDefault')}</ActionBtn>}
                    <ActionBtn danger onClick={() => setDeleteRow(v)}>{t('common.delete')}</ActionBtn>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <Modal
        open={modalOpen}
        title={editing ? t('versions.editTitle') : t('versions.createTitle')}
        onClose={() => setModalOpen(false)}
        footer={
          <>
            <button onClick={() => setModalOpen(false)} className="rounded px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-700">
              {t('common.cancel')}
            </button>
            <button
              disabled={!canSubmit}
              onClick={submit}
              className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-40"
            >
              {t('common.save')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
          {!editing && (
            <>
              <Field label={t('fields.service')}>
                <select value={form.svcId} onChange={(e) => loadFormDeploys(e.target.value)} className={inputCls}>
                  <option value="">{t('releases.selectService')}</option>
                  {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
                </select>
              </Field>
              <Field label={t('fields.deploymentName')}>
                <select value={form.depId} disabled={!formDeploys.length} onChange={(e) => setForm({ ...form, depId: e.target.value })} className={inputCls}>
                  <option value="">{t('releases.selectDeployment')}</option>
                  {formDeploys.map((d) => <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>)}
                </select>
              </Field>
            </>
          )}
          <Field label={t('fields.version')}>
            <input value={form.version} disabled={!!editing} onChange={(e) => setForm({ ...form, version: e.target.value })} className={inputCls} />
          </Field>
          <Field label={t('fields.image')}>
            <input value={form.image} onChange={(e) => setForm({ ...form, image: e.target.value })} placeholder="nginx:alpine" className={inputCls} />
          </Field>
          <Field label={t('fields.replicas')}>
            <input type="number" min={0} value={form.replicas} onChange={(e) => setForm({ ...form, replicas: e.target.value })} className={inputCls} />
          </Field>
          <Field label={t('fields.containerPort')}>
            <input type="number" min={0} max={65535} value={form.port} onChange={(e) => setForm({ ...form, port: e.target.value })} className={inputCls} />
          </Field>
          <Field label={t('fields.weight')}>
            <input type="number" min={0} max={1000} value={form.weight} onChange={(e) => setForm({ ...form, weight: e.target.value })} className={inputCls} />
          </Field>
          <Field label={t('fields.status')}>
            <select value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value })} className={inputCls}>
              <option value="stable">stable</option>
              <option value="canary">canary</option>
              <option value="standby">standby</option>
              <option value="inactive">inactive</option>
            </select>
          </Field>
          <Field label={t('fields.healthPath')}>
            <input value={form.health_path} onChange={(e) => setForm({ ...form, health_path: e.target.value })} placeholder="/health" className={inputCls} />
          </Field>
          <Field label={t('fields.env')}>
            <textarea rows={3} value={form.env} onChange={(e) => setForm({ ...form, env: e.target.value })} placeholder={'KEY=VALUE'} className={inputCls} />
          </Field>
          <Field label={t('fields.nodeSelector')}>
            <textarea rows={3} value={form.node_selector} onChange={(e) => setForm({ ...form, node_selector: e.target.value })} placeholder={'region=cn-east'} className={inputCls} />
          </Field>
        </div>

        {/* 绑定挂载：把节点数据目录下的子目录映射进容器，用于持久化与共享数据。 */}
        <div className="mt-4 border-t border-slate-200 pt-3 dark:border-slate-700">
          <div className="mb-2 flex items-center justify-between">
            <span className="text-sm font-medium text-slate-700 dark:text-slate-200">{t('versions.mounts')}</span>
            <button
              type="button"
              onClick={addMount}
              className="rounded border border-slate-300 px-2 py-1 text-xs text-slate-600 hover:bg-slate-100 dark:border-slate-600 dark:text-slate-300 dark:hover:bg-slate-700"
            >
              + {t('versions.addMount')}
            </button>
          </div>
          <p className="mb-2 text-xs text-slate-500 dark:text-slate-400">{t('versions.mountsHint')}</p>
          {form.mounts.length === 0 && (
            <p className="text-xs text-slate-400 dark:text-slate-500">{t('versions.noMounts')}</p>
          )}
          <div className="space-y-2">
            {form.mounts.map((m, i) => (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <input
                  value={m.path}
                  onChange={(e) => setMount(i, { path: e.target.value })}
                  placeholder={t('versions.mountPathPh')}
                  className={`${fieldCls} min-w-[12rem] flex-1`}
                />
                <input
                  value={m.target}
                  onChange={(e) => setMount(i, { target: e.target.value })}
                  placeholder={t('versions.mountTargetPh')}
                  className={`${fieldCls} min-w-[12rem] flex-1`}
                />
                <label className="flex items-center gap-1 text-xs text-slate-600 dark:text-slate-300">
                  <input type="checkbox" checked={!!m.read_only} onChange={(e) => setMount(i, { read_only: e.target.checked })} />
                  {t('versions.mountReadOnly')}
                </label>
                <button
                  type="button"
                  onClick={() => removeMount(i)}
                  className="rounded px-2 py-1 text-xs text-rose-600 hover:bg-rose-50 dark:text-rose-400 dark:hover:bg-rose-900/30"
                >
                  {t('common.delete')}
                </button>
              </div>
            ))}
          </div>
        </div>
      </Modal>

      <ConfirmDialog
        open={!!deleteRow}
        title={t('versions.deleteTitle')}
        message={t('versions.deleteConfirm')}
        onCancel={() => setDeleteRow(null)}
        onConfirm={doDelete}
      />
    </div>
  );
}
