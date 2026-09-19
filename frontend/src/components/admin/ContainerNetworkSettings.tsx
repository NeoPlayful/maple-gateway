// 系统级容器网络默认设置：默认 IPv4 池、项目前缀、分配模式、复用开关与冷却时间。
// 保存经 PUT /api/admin/system/container-network，只影响未来新节点的默认池（文档第59节/第61节）。
import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { api } from '../../lib/client';
import type { ContainerNetworkSettings } from '../../types';

const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

// 解析 IPv4 CIDR 的前缀长度；非法返回 null。
function cidrPrefix(s: string): number | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})\/(\d{1,2})$/.exec(s.trim());
  if (!m) return null;
  const octets = [m[1], m[2], m[3], m[4]].map((x) => Number(x));
  if (octets.some((o) => o < 0 || o > 255)) return null;
  const p = Number(m[5]);
  if (p < 0 || p > 32) return null;
  return p;
}

export default function ContainerNetworkSettings() {
  const { t } = useTranslation('admin');
  const [cfg, setCfg] = useState<ContainerNetworkSettings | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  const load = async () => {
    try {
      setCfg(await api.get<ContainerNetworkSettings>('/api/admin/system/container-network'));
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  };

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 实时容量预览：2^(project_prefix - pool_prefix)。
  const capacity = useMemo(() => {
    if (!cfg) return null;
    const pp = cidrPrefix(cfg.default_network_pool);
    if (pp === null) return null;
    const diff = cfg.default_project_prefix - pp;
    if (diff < 1 || diff > 30) return null;
    return Math.pow(2, diff);
  }, [cfg]);

  const update = (patch: Partial<ContainerNetworkSettings>) => {
    setCfg((cur) => (cur ? { ...cur, ...patch } : cur));
  };

  const save = async () => {
    if (!cfg) return;
    setErr('');
    setMsg('');
    setBusy(true);
    try {
      const saved = await api.put<ContainerNetworkSettings>('/api/admin/system/container-network', cfg);
      setCfg(saved);
      setMsg(t('settings.containerNet.saved'));
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('settings.containerNet.saveFailed'));
    } finally {
      setBusy(false);
    }
  };

  if (!cfg) {
    return (
      <div>
        {err && (
          <p className="rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
            {err}
          </p>
        )}
      </div>
    );
  }

  return (
    <div>
      <div className="mb-2 flex items-center gap-2">
        <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-200">
          {t('settings.containerNet.title')}
        </h2>
      </div>
      {msg && (
        <p className="mb-3 rounded bg-th-accent-soft-bg px-3 py-2 text-sm text-th-accent-soft-text">{msg}</p>
      )}
      {err && (
        <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">
          {err}
        </p>
      )}
      <p className="mb-4 text-xs text-slate-500 dark:text-slate-400">{t('settings.containerNet.hint')}</p>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
        <div className="flex flex-col gap-1">
          <label className="text-sm text-slate-600 dark:text-slate-300">
            {t('settings.containerNet.defaultPool')}
          </label>
          <input
            value={cfg.default_network_pool}
            onChange={(e) => update({ default_network_pool: e.target.value })}
            placeholder="10.128.0.0/9"
            className={`${inputCls} font-mono`}
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-sm text-slate-600 dark:text-slate-300">
            {t('settings.containerNet.projectPrefix')}
          </label>
          <input
            type="number"
            value={cfg.default_project_prefix}
            onChange={(e) => update({ default_project_prefix: Number(e.target.value) || 0 })}
            className={`${inputCls} font-mono`}
          />
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-sm text-slate-600 dark:text-slate-300">
            {t('settings.containerNet.allocationMode')}
          </label>
          <select
            value={cfg.allocation_mode}
            onChange={(e) => update({ allocation_mode: e.target.value })}
            className={inputCls}
          >
            <option value="sequential">{t('settings.containerNet.allocationSequential')}</option>
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label className="text-sm text-slate-600 dark:text-slate-300">
            {t('settings.containerNet.reuseDelay')}
          </label>
          <input
            type="number"
            value={cfg.default_reuse_delay_seconds}
            onChange={(e) => update({ default_reuse_delay_seconds: Number(e.target.value) || 0 })}
            className={`${inputCls} font-mono`}
          />
        </div>
        <div className="flex items-center gap-2">
          <input
            id="reuse-enabled"
            type="checkbox"
            checked={cfg.default_reuse_enabled}
            onChange={(e) => update({ default_reuse_enabled: e.target.checked })}
            className="h-4 w-4 accent-th-accent"
          />
          <label htmlFor="reuse-enabled" className="text-sm text-slate-600 dark:text-slate-300">
            {t('settings.containerNet.reuseEnabled')}
          </label>
        </div>
      </div>

      <p className="mt-3 text-xs text-slate-500 dark:text-slate-400">
        {capacity === null
          ? t('settings.containerNet.capacityInvalid')
          : t('settings.containerNet.capacityPreview', { n: capacity })}
      </p>

      <div className="mt-4 flex gap-3">
        <button
          onClick={save}
          disabled={busy}
          className="rounded bg-th-accent px-4 py-1.5 text-sm text-white hover:bg-th-accent-hover disabled:opacity-50"
        >
          {busy ? t('common.saving') : t('common.save')}
        </button>
        <button
          onClick={load}
          className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
        >
          {t('common.refresh')}
        </button>
      </div>
    </div>
  );
}
