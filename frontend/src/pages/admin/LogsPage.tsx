import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccessLogRow, ErrorLogRow, AuditLogRow } from '../../types';
import { PageHeader } from '../../themes';

type Tab = 'access' | 'error' | 'audit';

export default function LogsPage() {
  const { t } = useTranslation('admin');
  const [tab, setTab] = useState<Tab>('access');
  const [rows, setRows] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [host, setHost] = useState('');
  const [status, setStatus] = useState('');
  const [err, setErr] = useState('');

  const load = useCallback(async () => {
    setErr('');
    const qs = new URLSearchParams({ limit: '200' });
    if (host) qs.set('host', host);
    if (status) qs.set('status', status);
    try {
      const path = `/api/admin/logs/${tab}?${qs.toString()}`;
      const resp = await fetch(path, {
        headers: { Authorization: `Bearer ${localStorage.getItem('maple_token') ?? ''}` },
      });
      const j = await resp.json();
      if (j.code !== 'OK') {
        setErr(j.message || t('common.loadFailed'));
        return;
      }
      setRows((j.data ?? []) as any[]);
      setTotal(Number(j.meta?.total ?? 0));
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    }
  }, [tab, host, status, t]);

  const refresh = () => load();

  const theadCell = 'px-4 py-2';
  const rowCls = 'border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40';
  const tdCell = 'px-4 py-1.5';
  const timeTxt = 'text-xs text-slate-500 dark:text-slate-400';

  return (
    <div>
      <PageHeader
        title={t('logs.title')}
        right={
          <div className="flex gap-1">
            {(
              [
                ['access', 'logs.tabAccess'],
                ['error', 'logs.tabError'],
                ['audit', 'logs.tabAudit'],
              ] as [Tab, string][]
            ).map(([v, labelKey]) => (
              <button
                key={v}
                onClick={() => setTab(v)}
                className={`rounded px-3 py-1.5 text-sm ${
                  tab === v
                    ? 'bg-slate-800 text-white dark:bg-slate-600'
                    : 'bg-slate-200 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600'
                }`}
              >
                {t(labelKey)}
              </button>
            ))}
          </div>
        }
      />

      <div className="mb-4 flex flex-wrap items-end gap-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-700 dark:bg-slate-800">
        <div>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">Host</label>
          <input
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder={t('common.emptyPlaceholder')}
            className="w-48 rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
          />
        </div>
        <div>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.statusCode')}</label>
          <input
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            placeholder={t('logs.statusPh')}
            className="w-24 rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
          />
        </div>
        <button onClick={refresh} className="rounded bg-slate-800 px-4 py-1.5 text-sm text-white hover:bg-slate-700 dark:bg-slate-600 dark:hover:bg-slate-500">
          {t('common.search')}
        </button>
        <span className="ml-auto text-xs text-slate-400 dark:text-slate-500">
          {t('logs.bufferInfo', { total, latest: tab === 'audit' ? t('logs.persisted') : '200' })}
        </span>
      </div>

      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      <div className="overflow-x-auto rounded-xl border border-slate-200 bg-white shadow-sm dark:border-slate-700 dark:bg-slate-800">
        {tab === 'access' && (
          <table className="w-full text-sm">
            <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
              <tr>
                <th className={theadCell}>{t('logs.time')}</th>
                <th className={theadCell}>Host</th>
                <th className={theadCell}>Method</th>
                <th className={theadCell}>Path</th>
                <th className={theadCell}>{t('fields.status')}</th>
                <th className={theadCell}>IP</th>
                <th className={theadCell}>{t('logs.latencyMs')}</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AccessLogRow[]).map((r, i) => (
                <tr key={i} className={rowCls}>
                  <td className={`${tdCell} ${timeTxt}`}>{new Date(r.timestamp).toLocaleTimeString()}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.host}</td>
                  <td className={tdCell}>{r.method}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.path}</td>
                  <td className={tdCell}>{r.status}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.client_ip}</td>
                  <td className={tdCell}>{r.duration_ms}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {tab === 'error' && (
          <table className="w-full text-sm">
            <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
              <tr>
                <th className={theadCell}>{t('logs.time')}</th>
                <th className={theadCell}>Host</th>
                <th className={theadCell}>Path</th>
                <th className={theadCell}>{t('fields.status')}</th>
                <th className={theadCell}>{t('logs.error')}</th>
              </tr>
            </thead>
            <tbody>
              {(rows as ErrorLogRow[]).map((r, i) => (
                <tr key={i} className={rowCls}>
                  <td className={`${tdCell} ${timeTxt}`}>{new Date(r.timestamp).toLocaleTimeString()}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.host}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.path}</td>
                  <td className={tdCell}>{r.status}</td>
                  <td className={`${tdCell} text-xs text-rose-600`}>{r.error}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {tab === 'audit' && (
          <table className="w-full text-sm">
            <thead className="border-b border-slate-200 bg-slate-50 text-left text-xs text-slate-500 dark:border-slate-700 dark:bg-slate-900/50 dark:text-slate-400">
              <tr>
                <th className={theadCell}>{t('logs.time')}</th>
                <th className={theadCell}>Action</th>
                <th className={theadCell}>{t('logs.type')}</th>
                <th className={theadCell}>{t('logs.target')}</th>
                <th className={theadCell}>IP</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AuditLogRow[]).map((r) => (
                <tr key={r.id} className={rowCls}>
                  <td className={`${tdCell} ${timeTxt}`}>{new Date(r.created_at).toLocaleString()}</td>
                  <td className={tdCell}>{r.action}</td>
                  <td className={tdCell}>{r.target_type}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.target_id?.slice(0, 8) ?? '-'}</td>
                  <td className={`${tdCell} font-mono text-xs`}>{r.ip ?? '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {rows.length === 0 && <p className="px-4 py-8 text-center text-sm text-slate-400 dark:text-slate-500">{t('logs.none')}</p>}
      </div>
    </div>
  );
}
