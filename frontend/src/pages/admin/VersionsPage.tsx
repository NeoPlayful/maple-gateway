import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import type { Deployment, Service, Version } from '../../types';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { Field } from '../../components/admin/Field';
import { Modal } from '../../components/admin/Modal';
import { ConfirmDialog } from '../../components/admin/ConfirmDialog';
import { PageHeader } from '../../themes';

// 版本管理：为部署定义"可执行规格"（镜像 / 副本数 / 端口 / 环境变量 / 调度约束）。
// 版本规格即 Container Manager 的部署意图来源——保存后由 Gateway Leader 推给 CM。
export default function VersionsPage() {
  const { t } = useTranslation('admin');
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');
  const [modalOpen, setModalOpen] = useState(false);
  const [editing, setEditing] = useState<Version | null>(null);
  const [deleteId, setDeleteId] = useState<string | null>(null);

  // 表单态（字符串保存原始输入，提交时转换）。
  const [form, setForm] = useState({
    version: '',
    image: '',
    replicas: '1',
    port: '',
    weight: '100',
    status: 'stable',
    health_path: '',
    env: '',
    node_selector: '',
  });

  const loadServices = useCallback(async () => {
    try {
      setServices(await api.get<Service[]>('/api/admin/services?limit=100'));
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { loadServices(); }, [loadServices]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setVersions([]);
    if (!serviceId) { setDeploys([]); return; }
    try {
      setDeploys(await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=100`));
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    if (!deploymentId) { setVersions([]); return; }
    try {
      setVersions(await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`));
    } catch { setVersions([]); }
  };

  const openCreate = () => {
    setEditing(null);
    setForm({ version: '', image: '', replicas: '1', port: '', weight: '100', status: 'stable', health_path: '', env: '', node_selector: '' });
    setModalOpen(true);
  };

  const openEdit = (v: Version) => {
    setEditing(v);
    setForm({
      version: v.version,
      image: v.image ?? '',
      replicas: String(v.replicas ?? 1),
      port: v.port ? String(v.port) : '',
      weight: String(v.weight ?? 100),
      status: v.status,
      health_path: v.health_path ?? '',
      env: v.env ? Object.entries(v.env).map(([k, val]) => `${k}=${val}`).join('\n') : '',
      node_selector: v.node_selector ? Object.entries(v.node_selector).map(([k, val]) => `${k}=${val}`).join('\n') : '',
    });
    setModalOpen(true);
  };

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
      };
      if (editing) {
        await api.patch(`/api/admin/versions/${editing.id}`, body);
        setMsg(t('versions.updateHint'));
      } else {
        await api.post(`/api/admin/deployments/${depId}/versions`, body);
        setMsg(t('versions.createHint'));
      }
      setModalOpen(false);
      await loadVersions(depId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const setDefault = async (v: Version) => {
    setErr('');
    try {
      await api.post(`/api/admin/versions/${v.id}/default`, { deployment_id: depId });
      setMsg(t('versions.defaultHint'));
      await loadVersions(depId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async () => {
    if (!deleteId) return;
    try {
      await api.delete(`/api/admin/versions/${deleteId}`);
      setMsg(t('versions.deleteHint'));
      setDeleteId(null);
      await loadVersions(depId);
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <PageHeader
        title={t('versions.title')}
        right={
          <button
            disabled={!depId}
            onClick={openCreate}
            className="rounded bg-th-accent px-3 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-40"
          >
            + {t('versions.newVersion')}
          </button>
        }
      />
      {msg && <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      <div className="mb-4 grid grid-cols-1 gap-3 md:grid-cols-2">
        <Field label={t('fields.service')}>
          <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className={inputCls}>
            <option value="">{t('canary.selectService')}</option>
            {services.map((s) => <option key={s.id} value={s.id}>{s.name}</option>)}
          </select>
        </Field>
        <Field label={t('fields.deploymentName')}>
          <select value={depId} disabled={!deploys.length} onChange={(e) => loadVersions(e.target.value)} className={inputCls}>
            <option value="">{t('canary.selectDeployment')}</option>
            {deploys.map((d) => <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>)}
          </select>
        </Field>
      </div>

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
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
            {versions.length === 0 && (
              <tr><td colSpan={8} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('versions.none')}</td></tr>
            )}
            {versions.map((v) => (
              <tr key={v.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{v.version}</td>
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
                    <ActionBtn danger onClick={() => setDeleteId(v.id)}>{t('common.delete')}</ActionBtn>
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
              disabled={!form.version || !form.image}
              onClick={submit}
              className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-40"
            >
              {t('common.save')}
            </button>
          </>
        }
      >
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
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
            <input value={form.health_path} onChange={(e) => setForm({ ...form, health_path: e.target.value })} placeholder="/healthz" className={inputCls} />
          </Field>
          <Field label={t('fields.env')}>
            <textarea rows={3} value={form.env} onChange={(e) => setForm({ ...form, env: e.target.value })} placeholder={'KEY=VALUE'} className={inputCls} />
          </Field>
          <Field label={t('fields.nodeSelector')}>
            <textarea rows={3} value={form.node_selector} onChange={(e) => setForm({ ...form, node_selector: e.target.value })} placeholder={'region=cn-east'} className={inputCls} />
          </Field>
        </div>
      </Modal>

      <ConfirmDialog
        open={!!deleteId}
        title={t('versions.deleteTitle')}
        message={t('versions.deleteConfirm')}
        onCancel={() => setDeleteId(null)}
        onConfirm={doDelete}
      />
    </div>
  );
}
