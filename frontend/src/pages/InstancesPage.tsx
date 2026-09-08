import { useCallback, useEffect, useState } from 'react';
import { list, create, statusAction } from '../lib/modules';
import type { Service, Deployment, Instance as Inst, Version, Node } from '../types';
import { StatusBadge, ActionBtn, Field } from '../components/ui';

export default function InstancesPage() {
  const [rows, setRows] = useState<Inst[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [deploys, setDeploys] = useState<Deployment[]>([]);
  const [versions, setVersions] = useState<Version[]>([]);
  const [nodes, setNodes] = useState<Node[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  // 创建表单
  const [serviceId, setServiceId] = useState('');
  const [depId, setDepId] = useState('');
  const [verId, setVerId] = useState('');
  const [nodeId, setNodeId] = useState('');
  const [address, setAddress] = useState('');
  const [port, setPort] = useState('8080');
  const [weight, setWeight] = useState('1');

  const load = useCallback(async () => {
    try {
      setRows(await list('/api/admin/instances'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  const loadMeta = useCallback(async () => {
    try {
      const [s, d, n] = await Promise.all([
        list<Service>('/api/admin/services'),
        list<Deployment>('/api/admin/deployments'),
        list<Node>('/api/admin/nodes'),
      ]);
      setServices(s);
      setDeploys(d);
      setNodes(n);
    } catch {
      /* 忽略元数据加载失败 */
    }
  }, []);

  useEffect(() => {
    load();
    loadMeta();
  }, [load, loadMeta]);

  const loadVersions = async (deploymentId: string) => {
    setDepId(deploymentId);
    setVerId('');
    if (!deploymentId) {
      setVersions([]);
      return;
    }
    try {
      setVersions(await list<Version>(`/api/admin/deployments/${deploymentId}/versions`));
    } catch {
      setVersions([]);
    }
  };

  const svcName = (id: string) => services.find((s) => s.id === id)?.name ?? id.slice(0, 8);
  const nodeName = (id?: string | null) => nodes.find((n) => n.id === id)?.name ?? id?.slice(0, 8) ?? '-';

  const act = async (id: string, a: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction('/api/admin/instances', id, a);
      setMsg('操作成功');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '操作失败');
    }
  };

  const submit = async () => {
    setErr('');
    if (!serviceId || !address) {
      setErr('请填写服务与地址');
      return;
    }
    try {
      await create('/api/admin/instances/register', {
        service_id: serviceId,
        ...(depId ? { deployment_id: depId } : {}),
        ...(verId ? { version_id: verId } : {}),
        ...(nodeId ? { node_id: nodeId } : {}),
        address,
        port: Number(port),
        weight: Number(weight) || 1,
      });
      setMsg('注册成功');
      setOpen(false);
      setAddress('');
      setServiceId('');
      setDepId('');
      setVerId('');
      setNodeId('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : '注册失败');
    }
  };

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800">实例管理</h1>
        <button
          onClick={() => setOpen((v) => !v)}
          className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700"
        >
          {open ? '收起' : '+ 注册实例'}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl bg-white p-5 shadow-sm">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <Field label="所属服务">
              <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择服务</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </Field>
            <Field label="部署 (可选)">
              <select value={depId} onChange={(e) => loadVersions(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">直挂 Service</option>
                {deploys.filter((d) => d.service_id === serviceId).map((d) => (
                  <option key={d.id} value={d.id}>{d.name}</option>
                ))}
              </select>
            </Field>
            <Field label="版本 (可选)">
              <select value={verId} disabled={!depId} onChange={(e) => setVerId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">选择版本</option>
                {versions.map((v) => (
                  <option key={v.id} value={v.id}>{v.version}</option>
                ))}
              </select>
            </Field>
            <Field label="节点 (可选)">
              <select value={nodeId} onChange={(e) => setNodeId(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm">
                <option value="">本机</option>
                {nodes.map((n) => (
                  <option key={n.id} value={n.id}>{n.name}</option>
                ))}
              </select>
            </Field>
            <Field label="地址 (IP)">
              <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="如 10.0.0.5" className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="端口">
              <input type="number" value={port} onChange={(e) => setPort(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
            <Field label="权重">
              <input type="number" value={weight} onChange={(e) => setWeight(e.target.value)} className="w-full rounded border px-2 py-1.5 text-sm" />
            </Field>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700">
            注册
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl bg-white shadow-sm">
        <table className="w-full text-sm">
          <thead className="border-b bg-slate-50 text-left text-xs text-slate-500">
            <tr>
              <th className="px-4 py-2">服务</th>
              <th className="px-4 py-2">地址</th>
              <th className="px-4 py-2">节点</th>
              <th className="px-4 py-2">健康</th>
              <th className="px-4 py-2">状态</th>
              <th className="px-4 py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400">暂无实例</td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b hover:bg-slate-50">
                <td className="px-4 py-2">
                  {svcName(r.service_id)}
                  <span className="ml-1 text-xs text-slate-400">{r.version}</span>
                </td>
                <td className="px-4 py-2 font-mono text-xs">{r.address}:{r.port}</td>
                <td className="px-4 py-2">{nodeName(r.node_id)}</td>
                <td className="px-4 py-2"><StatusBadge value={r.health} /></td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>启用</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>禁用</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'drain')}>排空</ActionBtn>}
                    {r.status === 'draining' && <ActionBtn onClick={() => act(r.id, 'undrain')}>取消排空</ActionBtn>}
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
