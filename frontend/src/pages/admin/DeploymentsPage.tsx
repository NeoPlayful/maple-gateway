import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import CrudPage, { PageDef } from '../../components/CrudPage';
import { Modal } from '../../components/admin/Modal';
import { ActionBtn } from '../../components/admin/ActionBtn';
import { StatusBadge } from '../../components/admin/StatusBadge';
import { api, ApiError } from '../../lib/client';
import { list } from '../../lib/modules';
import type { CMOverview, Instance, Version } from '../../types';

// 实例行：展示 版本 → 实例 下钻结果，并提供重启/停止/启动与日志查看。
function InstanceRow({
  inst,
  nodeName,
  onAction,
  onLogs,
}: {
  inst: Instance;
  nodeName: string;
  onAction: (id: string, act: string) => void;
  onLogs: (id: string) => void;
}) {
  const { t } = useTranslation('admin');
  return (
    <div className="flex items-center justify-between rounded-lg border border-slate-200 bg-white px-3 py-2 text-xs dark:border-slate-700 dark:bg-slate-800">
      <div className="flex min-w-0 items-center gap-3">
        <span className={`h-2 w-2 shrink-0 rounded-full ${inst.health === 'healthy' ? 'bg-emerald-500' : inst.health === 'unhealthy' ? 'bg-rose-500' : 'bg-slate-400'}`} />
        <span className="font-mono text-slate-600 dark:text-slate-300">{inst.id.slice(0, 8)}</span>
        <span className="text-slate-400">{nodeName}</span>
        <span className="font-mono text-slate-400">{inst.address}:{inst.port}</span>
        <StatusBadge value={inst.health} />
        <StatusBadge value={inst.status} />
      </div>
      <div className="flex shrink-0 gap-1">
        <ActionBtn onClick={() => onLogs(inst.id)}>{t('deployments.logs')}</ActionBtn>
        <ActionBtn onClick={() => onAction(inst.id, 'restart')}>{t('deployments.restart')}</ActionBtn>
        {inst.health === 'healthy'
          ? <ActionBtn onClick={() => onAction(inst.id, 'stop')}>{t('deployments.stop')}</ActionBtn>
          : <ActionBtn onClick={() => onAction(inst.id, 'start')}>{t('deployments.start')}</ActionBtn>}
      </div>
    </div>
  );
}

// 部署详情：按版本分组展示实例，并叠加 CM 编排进度。
function DeploymentDetail({ deploymentId }: { deploymentId: string }) {
  const { t } = useTranslation('admin');
  const [versions, setVersions] = useState<Version[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [nodes, setNodes] = useState<{ id: string; name: string }[]>([]);
  const [phases, setPhases] = useState<Record<string, string>>({});
  const [cmEnabled, setCmEnabled] = useState(true);
  const [log, setLog] = useState<{ id: string; text: string } | null>(null);
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    try {
      const [vs, is, ns] = await Promise.all([
        list<Version>(`/api/admin/deployments/${deploymentId}/versions`),
        list<Instance>('/api/admin/instances'),
        list<{ id: string; name: string }>('/api/admin/nodes'),
      ]);
      setVersions(vs);
      setInstances(is.filter((i) => i.deployment_id === deploymentId));
      setNodes(ns);
      setErr('');
      // CM 编排进度（未接入则静默降级）。
      try {
        const ov = await api.get<CMOverview>('/api/admin/cm/overview');
        const p: Record<string, string> = {};
        Object.entries(ov.deployments ?? {}).forEach(([id, ph]) => { p[id] = ph.status; });
        setPhases(p);
        setCmEnabled(true);
      } catch (e) {
        if (e instanceof ApiError && e.status === 503) setCmEnabled(false);
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [deploymentId, t]);

  useEffect(() => { load(); }, [load]);

  const nodeName = (id?: string | null) => nodes.find((n) => n.id === id)?.name ?? (id ? id.slice(0, 8) : '-');

  const act = async (id: string, a: string) => {
    try {
      await api.post(`/api/admin/cm/instances/${id}/${a}`);
      toast.success(t('common.operateSuccess'));
      await load();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const openLogs = async (id: string) => {
    setLog({ id, text: t('deployments.loading') });
    try {
      const res = await api.get<{ logs: string }>(`/api/admin/cm/instances/${id}/logs?tail=200`);
      setLog({ id, text: res.logs || '' });
    } catch (e) {
      setLog({ id, text: e instanceof Error ? e.message : t('common.loadFailed') });
    }
  };

  if (err) return <p className="text-xs text-rose-600 dark:text-rose-300">{err}</p>;

  const phase = phases[deploymentId];

  return (
    <div className="space-y-3">
      {!cmEnabled && (
        <p className="rounded bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
          {t('runtime.notEnabled')}
        </p>
      )}
      {cmEnabled && phase && (
        <div className="flex items-center gap-2 text-xs text-slate-500 dark:text-slate-400">
          <span>{t('deployments.phase')}:</span>
          <StatusBadge value={phase} raw />
        </div>
      )}
      {versions.length === 0 && <p className="text-xs text-slate-400">{t('deployments.noVersions')}</p>}
      {versions.map((v) => {
        const insts = instances.filter((i) => i.version_id === v.id);
        return (
          <div key={v.id}>
            <div className="mb-2 flex items-center gap-2 text-xs">
              <span className="font-semibold text-slate-700 dark:text-slate-200">{v.version}</span>
              <span className="font-mono text-slate-400">{v.image ?? '-'}</span>
              <StatusBadge value={v.status} />
              <span className="text-slate-400">{insts.length} {t('deployments.instances')}</span>
            </div>
            {insts.length === 0
              ? <p className="pl-2 text-xs text-slate-400">{t('deployments.noInstances')}</p>
              : (
                <div className="space-y-1.5">
                  {insts.map((i) => (
                    <InstanceRow
                      key={i.id}
                      inst={i}
                      nodeName={nodeName(i.node_id)}
                      onAction={act}
                      onLogs={openLogs}
                    />
                  ))}
                </div>
              )}
          </div>
        );
      })}

      <Modal
        open={log !== null}
        title={`${t('deployments.logTitle')} · ${log?.id.slice(0, 8) ?? ''}`}
        onClose={() => setLog(null)}
        maxWidth="max-w-4xl"
      >
        <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-slate-900 p-4 font-mono text-xs leading-relaxed text-slate-100">
          {log?.text || t('deployments.noInstances')}
        </pre>
      </Modal>
    </div>
  );
}

const def: PageDef = {
  title: 'deployments.title',
  path: '/api/admin/deployments',
  columns: [
    { key: 'name', label: 'fields.deploymentName' },
    { key: 'service_id', label: 'fields.service', render: (r) => r.service_id?.slice(0, 8) ?? '-' },
    { key: 'strategy', label: 'fields.strategy' },
    { key: 'status', label: 'fields.status', badge: true },
  ],
  createFields: [
    {
      key: 'service_id',
      label: 'fields.parentService',
      required: true,
      loadOptions: { path: '/api/admin/services', valueKey: 'id', labelKey: 'name' },
    },
    { key: 'name', label: 'fields.deploymentName', required: true, placeholder: 'deployments.namePh' },
    {
      key: 'strategy',
      label: 'fields.deployStrategy',
      type: 'select',
      options: [
        { value: 'rolling', label: 'rolling' },
        { value: 'recreate', label: 'recreate' },
        { value: 'blue_green', label: 'blue_green' },
      ],
    },
  ],
  // 展开行：按版本下钻到实例，叠加编排进度与实例操作。
  renderDetail: (row) => <DeploymentDetail deploymentId={row.id} />,
};

export default function DeploymentsPage() {
  return <CrudPage def={def} />;
}
