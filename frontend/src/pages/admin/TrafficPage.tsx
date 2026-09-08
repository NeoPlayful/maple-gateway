import { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { list, create, statusAction, remove } from '../../lib/modules';
import type { TrafficPolicy, Service } from '../../types';
import { StatusBadge, ActionBtn, Field } from '../../components/ui';

export default function TrafficPage() {
  const { t } = useTranslation('admin');
  const [rows, setRows] = useState<TrafficPolicy[]>([]);
  const [services, setServices] = useState<Service[]>([]);
  const [open, setOpen] = useState(false);
  const [err, setErr] = useState('');
  const [msg, setMsg] = useState('');

  const [serviceId, setServiceId] = useState('');
  const [name, setName] = useState('');
  const [priority, setPriority] = useState('100');
  const [headerKey, setHeaderKey] = useState('');
  const [headerVal, setHeaderVal] = useState('');
  const [cookieKey, setCookieKey] = useState('');
  const [cookieVal, setCookieVal] = useState('');
  const [path, setPath] = useState('');
  const [pathExact, setPathExact] = useState('');
  const [percent, setPercent] = useState('');

  const load = useCallback(async () => {
    try {
      setRows(await list('/api/admin/traffic'));
      setErr('');
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [t]);

  useEffect(() => {
    load();
    list<Service>('/api/admin/services').then(setServices).catch(() => {});
  }, [load]);

  const act = async (id: string, a: string) => {
    setErr('');
    setMsg('');
    try {
      await statusAction('/api/admin/traffic', id, a);
      setMsg(t('common.operateSuccess'));
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.operateFailed'));
    }
  };

  const doDelete = async (id: string) => {
    if (!window.confirm(t('traffic.confirmDelete'))) return;
    try {
      await remove('/api/admin/traffic', id);
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.deleteFailed'));
    }
  };

  const matchDesc = (r: TrafficPolicy) => {
    const m = r.match ?? {};
    const parts: string[] = [];
    if (m.header) Object.entries(m.header).forEach(([k, v]) => parts.push(`header:${k}=${v}`));
    if (m.cookie) Object.entries(m.cookie).forEach(([k, v]) => parts.push(`cookie:${k}=${v}`));
    if (m.path) parts.push(`path:${m.path}`);
    if (m.path_exact) parts.push(`exact:${m.path_exact}`);
    if (m.percent) parts.push(`${m.percent}%`);
    return parts.length ? parts.join(' ') : t('traffic.fallbackWeight');
  };

  const submit = async () => {
    setErr('');
    if (!serviceId || !name) {
      setErr(t('traffic.errFill'));
      return;
    }
    const match: Record<string, unknown> = {};
    if (headerKey && headerVal) match.header = { [headerKey]: headerVal };
    if (cookieKey && cookieVal) match.cookie = { [cookieKey]: cookieVal };
    if (path) match.path = path;
    if (pathExact) match.path_exact = pathExact;
    if (percent) match.percent = Number(percent);
    try {
      await create('/api/admin/traffic', {
        service_id: serviceId,
        name,
        priority: Number(priority) || 100,
        match,
        status: 'enabled',
      });
      setMsg(t('common.createSuccess'));
      setOpen(false);
      setName('');
      setHeaderKey('');
      setHeaderVal('');
      setCookieKey('');
      setCookieVal('');
      setPath('');
      setPathExact('');
      setPercent('');
      await load();
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.createFailed'));
    }
  };

  const inputCls =
    'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200';

  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold text-slate-800 dark:text-slate-100">{t('traffic.title')}</h1>
        <button onClick={() => setOpen((v) => !v)} className="rounded bg-emerald-600 px-3 py-1.5 text-sm text-white hover:bg-emerald-700 dark:bg-emerald-600 dark:hover:bg-emerald-700">
          {open ? t('common.collapse') : `+ ${t('traffic.newPolicy')}`}
        </button>
      </div>
      {msg && <p className="mb-3 rounded bg-emerald-50 px-3 py-2 text-sm text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">{msg}</p>}
      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      {open && (
        <div className="mb-5 rounded-xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-700 dark:bg-slate-800">
          <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
            <Field label={t('fields.service')}>
              <select value={serviceId} onChange={(e) => setServiceId(e.target.value)} className={inputCls}>
                <option value="">{t('ratelimit.selectService')}</option>
                {services.map((s) => (
                  <option key={s.id} value={s.id}>{s.name}</option>
                ))}
              </select>
            </Field>
            <Field label={t('traffic.policyName')}>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder={t('traffic.namePh')} className={inputCls} />
            </Field>
            <Field label={t('traffic.priority')}>
              <input type="number" value={priority} onChange={(e) => setPriority(e.target.value)} className={inputCls} />
            </Field>
            <Field label={t('traffic.percent')}>
              <input type="number" value={percent} onChange={(e) => setPercent(e.target.value)} placeholder={t('traffic.percentPh')} className={inputCls} />
            </Field>
            <Field label={t('traffic.headerKey')}>
              <input value={headerKey} onChange={(e) => setHeaderKey(e.target.value)} placeholder={t('traffic.headerKeyPh')} className={inputCls} />
            </Field>
            <Field label={t('traffic.headerValue')}>
              <input value={headerVal} onChange={(e) => setHeaderVal(e.target.value)} placeholder={t('traffic.headerValuePh')} className={inputCls} />
            </Field>
            <Field label={t('traffic.cookieKey')}>
              <input value={cookieKey} onChange={(e) => setCookieKey(e.target.value)} className={inputCls} />
            </Field>
            <Field label={t('traffic.cookieValue')}>
              <input value={cookieVal} onChange={(e) => setCookieVal(e.target.value)} className={inputCls} />
            </Field>
            <Field label={t('traffic.pathPrefix')}>
              <input value={path} onChange={(e) => setPath(e.target.value)} placeholder={t('traffic.pathPh')} className={inputCls} />
            </Field>
            <Field label={t('traffic.pathExact')}>
              <input value={pathExact} onChange={(e) => setPathExact(e.target.value)} className={inputCls} />
            </Field>
          </div>
          <button onClick={submit} className="mt-4 rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-700 dark:hover:bg-slate-600">
            {t('common.create')}
          </button>
        </div>
      )}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <table className="w-full text-sm">
          <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
            <tr>
              <th className="px-4 py-2">{t('fields.name')}</th>
              <th className="px-4 py-2">{t('traffic.priority')}</th>
              <th className="px-4 py-2">{t('traffic.match')}</th>
              <th className="px-4 py-2">{t('traffic.target')}</th>
              <th className="px-4 py-2">{t('fields.status')}</th>
              <th className="px-4 py-2">{t('common.action')}</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-400 dark:text-slate-500">{t('traffic.none')}</td>
              </tr>
            )}
            {rows.map((r) => (
              <tr key={r.id} className="border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40">
                <td className="px-4 py-2 font-medium">{r.name}</td>
                <td className="px-4 py-2">{r.priority}</td>
                <td className="px-4 py-2 text-xs text-slate-600">{matchDesc(r)}</td>
                <td className="px-4 py-2 text-xs">{r.target_version_id ? r.target_version_id.slice(0, 8) : '-'}</td>
                <td className="px-4 py-2"><StatusBadge value={r.status} /></td>
                <td className="px-4 py-2">
                  <div className="flex flex-wrap gap-1">
                    {r.status !== 'enabled' && <ActionBtn onClick={() => act(r.id, 'enable')}>{t('common.enable')}</ActionBtn>}
                    {r.status === 'enabled' && <ActionBtn onClick={() => act(r.id, 'disable')}>{t('common.disable')}</ActionBtn>}
                    <ActionBtn danger onClick={() => doDelete(r.id)}>{t('common.delete')}</ActionBtn>
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
