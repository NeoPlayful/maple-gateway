import { useCallback, useEffect, useState } from 'react';
import { api } from '../lib/client';
import type { CanaryRelease, Deployment, Service, Version } from '../types';
import { PhaseBadge } from '../components/ui';

interface VersionMeta {
  id: string;
  version: string;
  deployment_id: string;
  service_id: string;
  weight: number;
  status: string;
}

export default function CanaryPage() {
  const [releases, setReleases] = useState<CanaryRelease[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  // 创建表单态。
  const [showCreate, setShowCreate] = useState(false);
  const [svcId, setSvcId] = useState('');
  const [depId, setDepId] = useState('');
  const [name, setName] = useState('');
  const [stableId, setStableId] = useState('');
  const [canaryId, setCanaryId] = useState('');
  const [initialWeight, setInitialWeight] = useState(10);

  const load = useCallback(async () => {
    try {
      const list = await api.get<CanaryRelease[]>('/api/admin/canary?limit=50');
      setReleases(list);
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  useEffect(() => {
    load();
    (async () => {
      try {
        const s = await api.get<Service[]>('/api/admin/services?limit=100');
        setServices(s);
      } catch { /* ignore */ }
    })();
  }, [load]);

  const loadDeploys = async (serviceId: string) => {
    setSvcId(serviceId);
    setDepId('');
    setStableId('');
    setCanaryId('');
    setVersions([]);
    try {
      const d = await api.get<Deployment[]>(`/api/admin/deployments?service_id=${serviceId}&limit=50`);
      setDeploys(d);
    } catch { setDeploys([]); }
  };

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setStableId('');
    setCanaryId('');
    try {
      const v = await api.get<Version[]>(`/api/admin/deployments/${deploymentId}/versions`);
      const metas = v.map((x) => ({ ...x, deployment_id: deploymentId, service_id: svcId }));
      setVersions(metas);
    } catch { setVersions([]); }
  };

  const action = async (id: string, act: string, body?: unknown) => {
    setMsg('');
    setErr('');
    try {
      await api.post(`/api/admin/canary/${id}/${act}`, body);
      setMsg(`操作 ${act} 成功`);
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '操作失败');
    }
  };

  const create = async () => {
    setErr('');
    try {
      await api.post('/api/admin/canary', {
        service_id: svcId,
        name,
        stable_version_id: stableId,
        canary_version_id: canaryId,
        initial_weight: initialWeight,
      });
      setMsg('创建成功，可在列表 start 开始发布');
      setShowCreate(false);
      setName('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '创建失败');
    }
  };

  const verName = (id: string) => versions.find((v) => v.id === id)?.version ?? id.slice(0, 8);
  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">Canary 发布</h1>
        <button
          onClick={() => setShowCreate((v) => !v)}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700"
        >
          {showCreate ? '收起' : '+ 新建 Canary'}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      {showCreate && (
        <div className="mb-5 rounded-xl bg-white p-5 shadow-sm">
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-slate-500">服务</label>
              <select value={svcId} onChange={(e) => loadDeploys(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择服务</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">名称</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 v2-canary" className="w-full rounded border px-2 py-1.5 text-sm" />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">部署</label>
              <select value={depId} disabled={!deploys.length} onChange={(e) => loadVersions(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择部署</option>
                {deploys.map((d) => (
                  <option key={d.id} value={d.id}>{d.name} ({d.strategy})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">初始权重 %</label>
              <input type="number" min={1} max={100} value={initialWeight} onChange={(e) => setInitialWeight(Number(e.target.value))} className="w-full rounded border px-2 py-1.5 text-sm" />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">Stable 版本</label>
              <select value={stableId} onChange={(e) => setStableId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择 stable</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                ))}
              </select>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500">Canary 版本</label>
              <select value={canaryId} onChange={(e) => setCanaryId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择 canary</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version} ({v.status}, w{v.weight})</option>
                ))}
              </select>
            </div>
          </div>
          <button
            disabled={!stableId || !canaryId || stableId === canaryId}
            onClick={create}
            className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 disabled:opacity-40"
          >
            创建
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
            <tr>
              <th className="px-4 py-2">名称</th>
              <th className="px-4 py-2">服务</th>
              <th className="px-4 py-2">版本</th>
              <th className="px-4 py-2">权重</th>
              <th className="px-4 py-2">阶段</th>
              <th className="px-4 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {releases.length === 0 && (
              <tr><td colSpan={6} className="px-4 py-6 text-center text-slate-400">暂无 Canary 发布</td></tr>
            )}
            {releases.map((r) => (
              <tr key={r.id} className="border-b hover:bg-slate-50">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">{svcName(r.service_id)}</td>
                <td className="px-4 py-2 text-xs">
                  stable {verName(r.stable_version_id)} → canary {verName(r.canary_version_id)}
                </td>
                <td className="px-4 py-2">{r.canary_weight}% / {r.target_weight}%</td>
                <td className="px-4 py-2"><PhaseBadge phase={r.phase} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.phase === 'created' && (
                      <ActionBtn onClick={() => action(r.id, 'start')}>开始</ActionBtn>
                    )}
                    {r.phase === 'running' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'pause')}>暂停</ActionBtn>
                        <ActionBtn onClick={() => setWeightPrompt(r.id)}>调权重</ActionBtn>
                        <ActionBtn onClick={() => action(r.id, 'promote')}>晋升</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>回滚</ActionBtn>
                      </>
                    )}
                    {r.phase === 'paused' && (
                      <>
                        <ActionBtn onClick={() => action(r.id, 'resume')}>恢复</ActionBtn>
                        <ActionBtn onClick={() => setWeightPrompt(r.id)}>调权重</ActionBtn>
                        <ActionBtn danger onClick={() => action(r.id, 'rollback')}>回滚</ActionBtn>
                      </>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );

  async function setWeightPrompt(releaseId: string) {
    const w = window.prompt('设置 canary 权重 (0-100):', '25');
    if (w === null) return;
    const n = Number(w);
    if (Number.isNaN(n) || n < 0 || n > 100) {
      setErr('权重须在 0-100');
      return;
    }
    await action(releaseId, 'weight', { weight: n });
  }
}

function ActionBtn({ onClick, danger, children }: { onClick: () => void; danger?: boolean; children: React.ReactNode }) {
  return (
    <button
      onClick={onClick}
      className={`rounded px-2 py-1 text-xs ${
        danger ? 'bg-rose-600 text-white hover:bg-rose-700' : 'bg-slate-200 text-slate-700 hover:bg-slate-300'
      }`}
    >
      {children}
    </button>
  );
}
