import { Fragment, useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccessLogRow, ErrorLogRow, AuditLogRow } from '../../types';
import { PageHeader } from '../../themes';
import { shortId } from '../../lib/ids';

type Tab = 'access' | 'error' | 'audit';

type Row = AccessLogRow | ErrorLogRow | AuditLogRow;

const PAGE_SIZES = [50, 100, 200, 500];
const AUTO_MS = 5000;

// toRFC3339 把 datetime-local 值转为后端可解析的 RFC3339；空值原样返回空串。
function toRFC3339(v: string): string {
  if (!v) return '';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? '' : d.toISOString();
}

export default function LogsPage() {
  const { t } = useTranslation('admin');
  const [tab, setTab] = useState<Tab>('access');

  // 访问/错误 Tab 的筛选。
  const [host, setHost] = useState('');
  const [status, setStatus] = useState('');
  const [requestId, setRequestId] = useState('');
  // 审计 Tab 的筛选。
  const [action, setAction] = useState('');
  const [targetType, setTargetType] = useState('');
  // 时间范围（两 Tab 共用）。
  const [from, setFrom] = useState('');
  const [to, setTo] = useState('');

  const [page, setPage] = useState(0);
  const [pageSize, setPageSize] = useState(100);

  const [rows, setRows] = useState<Row[]>([]);
  const [total, setTotal] = useState(0);
  const [count, setCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [auto, setAuto] = useState(false);
  const [err, setErr] = useState('');
  const [expanded, setExpanded] = useState<number | null>(null);

  const isAudit = tab === 'audit';
  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  const load = useCallback(async () => {
    setErr('');
    setLoading(true);
    const qs = new URLSearchParams({ limit: String(pageSize), offset: String(page * pageSize) });
    if (isAudit) {
      if (action) qs.set('action', action);
      if (targetType) qs.set('target_type', targetType);
    } else {
      if (host) qs.set('host', host);
      if (status) qs.set('status', status);
      if (requestId) qs.set('request_id', requestId);
    }
    const f = toRFC3339(from);
    const tt = toRFC3339(to);
    if (f) qs.set('from', f);
    if (tt) qs.set('to', tt);
    try {
      const resp = await fetch(`/api/admin/logs/${tab}?${qs.toString()}`, {
        headers: { Authorization: `Bearer ${localStorage.getItem('maple_token') ?? ''}` },
      });
      const j = await resp.json();
      if (j.code !== 'OK') {
        setErr(j.message || t('common.loadFailed'));
        return;
      }
      setRows((j.data ?? []) as Row[]);
      setTotal(Number(j.meta?.total ?? 0));
      setCount(Number(j.meta?.count ?? 0));
    } catch (e) {
      setErr(e instanceof Error ? e.message : t('common.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [tab, isAudit, host, status, requestId, action, targetType, from, to, page, pageSize, t]);

  // 首次进入与筛选/分页变化时加载。
  useEffect(() => {
    load();
  }, [load]);

  // 自动刷新：开启后按固定间隔取数；关闭即暂停。
  useEffect(() => {
    if (!auto) return;
    const id = window.setInterval(() => {
      load();
    }, AUTO_MS);
    return () => window.clearInterval(id);
  }, [auto, load]);

  // 切换 Tab：回到第一页并清空展开态。
  useEffect(() => {
    setPage(0);
    setExpanded(null);
  }, [tab]);

  const resetTo = (setter: (v: string) => void) => (v: string) => {
    setPage(0);
    setter(v);
  };

  const exportRows = (fmt: 'csv' | 'json') => {
    if (rows.length === 0) return;
    let content: string;
    let mime: string;
    if (fmt === 'json') {
      content = JSON.stringify(rows, null, 2);
      mime = 'application/json';
    } else {
      const keys = Object.keys(rows[0]);
      const esc = (v: unknown) => {
        const s = v == null ? '' : String(v);
        return /[",\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
      };
      content = [keys.join(','), ...rows.map((r) => keys.map((k) => esc((r as any)[k])).join(','))].join('\n');
      mime = 'text/csv';
    }
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `logs-${tab}-${Date.now()}.${fmt}`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const theadCell = 'px-4 py-2';
  const rowCls = 'border-b border-slate-200 hover:bg-slate-50 dark:border-slate-700 dark:hover:bg-slate-700/40';
  const tdCell = 'px-4 py-1.5';
  const timeTxt = 'text-xs text-slate-500 dark:text-slate-400';
  const inputCls =
    'rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200';

  const fmt = (ts: string) => new Date(ts).toLocaleString();

  const detailFields = (r: Row): [string, unknown][] => Object.entries(r);

  const renderDetail = (r: Row, i: number) =>
    expanded === i ? (
      <tr key={`d-${i}`} className="border-b border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40">
        <td colSpan={8} className="px-4 py-3">
          <dl className="grid grid-cols-1 gap-x-6 gap-y-1 sm:grid-cols-2">
            {detailFields(r).map(([k, v]) => (
              <div key={k} className="flex gap-2 text-xs">
                <dt className="w-28 shrink-0 text-slate-400 dark:text-slate-500">{k}</dt>
                <dd className="break-all font-mono text-slate-700 dark:text-slate-200">{v == null ? '-' : String(v)}</dd>
              </div>
            ))}
          </dl>
        </td>
      </tr>
    ) : null;

  return (
    <div>
      <PageHeader
        title={t('logs.title')}
        right={
          <div className="flex items-center gap-1">
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
            <button
              onClick={() => setAuto((a) => !a)}
              className={`ml-2 rounded px-3 py-1.5 text-sm ${
                auto
                  ? 'bg-emerald-600 text-white hover:bg-emerald-500'
                  : 'bg-slate-200 text-slate-600 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600'
              }`}
            >
              {auto ? t('logs.pause') : t('logs.autoRefresh')}
            </button>
          </div>
        }
      />

      <div className="mb-4 flex flex-wrap items-end gap-3 rounded-th-card border border-slate-200 bg-white p-4 shadow-th-card dark:border-slate-700 dark:bg-slate-800">
        {!isAudit && (
          <>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">Host</label>
              <input value={host} onChange={(e) => resetTo(setHost)(e.target.value)} placeholder={t('common.emptyPlaceholder')} className={`w-44 ${inputCls}`} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.statusCode')}</label>
              <input value={status} onChange={(e) => resetTo(setStatus)(e.target.value)} placeholder={t('logs.statusPh')} className={`w-24 ${inputCls}`} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.requestId')}</label>
              <input value={requestId} onChange={(e) => resetTo(setRequestId)(e.target.value)} placeholder={t('logs.requestIdPh')} className={`w-56 ${inputCls}`} />
            </div>
          </>
        )}
        {isAudit && (
          <>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.action')}</label>
              <input value={action} onChange={(e) => resetTo(setAction)(e.target.value)} placeholder={t('logs.actionPh')} className={`w-48 ${inputCls}`} />
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.targetType')}</label>
              <input value={targetType} onChange={(e) => resetTo(setTargetType)(e.target.value)} placeholder={t('logs.targetTypePh')} className={`w-40 ${inputCls}`} />
            </div>
          </>
        )}
        <div>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.from')}</label>
          <input type="datetime-local" value={from} onChange={(e) => resetTo(setFrom)(e.target.value)} className={inputCls} />
        </div>
        <div>
          <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{t('logs.to')}</label>
          <input type="datetime-local" value={to} onChange={(e) => resetTo(setTo)(e.target.value)} className={inputCls} />
        </div>
        <div className="ml-auto flex items-end gap-2">
          <button onClick={() => exportRows('csv')} disabled={rows.length === 0} className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-300 disabled:opacity-40 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">
            {t('logs.export')} CSV
          </button>
          <button onClick={() => exportRows('json')} disabled={rows.length === 0} className="rounded bg-slate-200 px-3 py-1.5 text-sm text-slate-700 hover:bg-slate-300 disabled:opacity-40 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600">
            {t('logs.export')} JSON
          </button>
        </div>
      </div>

      {err && <p className="mb-3 rounded bg-rose-50 px-3 py-2 text-sm text-rose-600 dark:bg-rose-900/40 dark:text-rose-300">{err}</p>}

      <div className="overflow-x-auto rounded-th-card border border-slate-200 bg-white shadow-th-card dark:border-slate-700 dark:bg-slate-800">
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
                <th className={theadCell}>{t('logs.requestId')}</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AccessLogRow[]).map((r, i) => (
                <Fragment key={i}>
                  <tr onClick={() => setExpanded(expanded === i ? null : i)} className={`${rowCls} cursor-pointer`}>
                    <td className={`${tdCell} ${timeTxt}`}>{fmt(r.timestamp)}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.host}</td>
                    <td className={tdCell}>{r.method}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.path}</td>
                    <td className={tdCell}>{r.status}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.client_ip}</td>
                    <td className={tdCell}>{r.duration_ms}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.request_id ? shortId(r.request_id) : '-'}</td>
                  </tr>
                  {renderDetail(r, i)}
                </Fragment>
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
                <th className={theadCell}>{t('logs.requestId')}</th>
              </tr>
            </thead>
            <tbody>
              {(rows as ErrorLogRow[]).map((r, i) => (
                <Fragment key={i}>
                  <tr onClick={() => setExpanded(expanded === i ? null : i)} className={`${rowCls} cursor-pointer`}>
                    <td className={`${tdCell} ${timeTxt}`}>{fmt(r.timestamp)}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.host}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.path}</td>
                    <td className={tdCell}>{r.status}</td>
                    <td className={`${tdCell} text-xs text-rose-600`}>{r.error}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{(r as any).request_id ? shortId((r as any).request_id) : '-'}</td>
                  </tr>
                  {renderDetail(r, i)}
                </Fragment>
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
                <th className={theadCell}>{t('logs.operator')}</th>
                <th className={theadCell}>IP</th>
              </tr>
            </thead>
            <tbody>
              {(rows as AuditLogRow[]).map((r, i) => (
                <Fragment key={r.id}>
                  <tr onClick={() => setExpanded(expanded === i ? null : i)} className={`${rowCls} cursor-pointer`}>
                    <td className={`${tdCell} ${timeTxt}`}>{fmt(r.created_at)}</td>
                    <td className={tdCell}>{r.action}</td>
                    <td className={tdCell}>{r.target_type}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.target_id ? shortId(r.target_id) : '-'}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.admin_id ? shortId(r.admin_id) : '-'}</td>
                    <td className={`${tdCell} font-mono text-xs`}>{r.ip ?? '-'}</td>
                  </tr>
                  {renderDetail(r, i)}
                </Fragment>
              ))}
            </tbody>
          </table>
        )}
        {rows.length === 0 && !loading && <p className="px-4 py-8 text-center text-sm text-slate-400 dark:text-slate-500">{t('logs.none')}</p>}
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm">
        <div className="flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-1.5 text-xs text-slate-400 dark:text-slate-500">
            {t('logs.pageSize')}
            <select
              value={pageSize}
              onChange={(e) => {
                setPage(0);
                setPageSize(Number(e.target.value));
              }}
              className="rounded border border-slate-300 bg-white px-2 py-1 text-xs text-slate-700 dark:border-slate-600 dark:bg-slate-900 dark:text-slate-200"
            >
              {PAGE_SIZES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>
          <span className="text-xs text-slate-400 dark:text-slate-500">{t('logs.matched', { total })}</span>
          <span className="text-xs text-slate-400 dark:text-slate-500">{isAudit ? t('logs.persistedCount', { count }) : t('logs.bufferCount', { count })}</span>
          {loading && <span className="text-xs text-slate-500 dark:text-slate-400">{t('logs.loading')}</span>}
          {auto && <span className="text-xs text-emerald-600 dark:text-emerald-400">{t('logs.autoOn')}</span>}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-xs text-slate-400 dark:text-slate-500">{t('logs.pageInfo', { page: page + 1, pages: totalPages })}</span>
          <button
            onClick={() => setPage((p) => Math.max(0, p - 1))}
            disabled={page === 0}
            className="rounded bg-slate-200 px-3 py-1.5 text-slate-700 hover:bg-slate-300 disabled:opacity-40 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
          >
            {t('logs.prev')}
          </button>
          <button
            onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
            disabled={page >= totalPages - 1}
            className="rounded bg-slate-200 px-3 py-1.5 text-slate-700 hover:bg-slate-300 disabled:opacity-40 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
          >
            {t('logs.next')}
          </button>
        </div>
      </div>
    </div>
  );
}
