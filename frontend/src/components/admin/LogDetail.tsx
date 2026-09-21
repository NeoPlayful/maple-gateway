// 请求日志的展开详情：请求摘要栏 + 详情卡片 + 原始日志面板。
// 仅负责「日志行展开后的展示」，不改动筛选/分页/接口等页面逻辑。
import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import {
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  DocumentTextIcon,
  GlobeAltIcon,
  ServerIcon,
  Square2StackIcon,
} from '@heroicons/react/24/outline';
import type { AccessLogRow, ErrorLogRow, AuditLogRow } from '../../types';

export type LogTab = 'access' | 'error' | 'audit';
export type LogRow = AccessLogRow | ErrorLogRow | AuditLogRow;

// HTTP 状态码 → 语义色 + 短语。2xx 绿 / 3xx 蓝 / 4xx 橙 / 5xx 红，其余灰。
const STATUS_PHRASE: Record<number, string> = {
  200: 'OK',
  201: 'Created',
  202: 'Accepted',
  204: 'No Content',
  206: 'Partial Content',
  301: 'Moved Permanently',
  302: 'Found',
  304: 'Not Modified',
  307: 'Temporary Redirect',
  308: 'Permanent Redirect',
  400: 'Bad Request',
  401: 'Unauthorized',
  403: 'Forbidden',
  404: 'Not Found',
  405: 'Method Not Allowed',
  408: 'Request Timeout',
  409: 'Conflict',
  410: 'Gone',
  413: 'Payload Too Large',
  415: 'Unsupported Media Type',
  422: 'Unprocessable Entity',
  429: 'Too Many Requests',
  500: 'Internal Server Error',
  502: 'Bad Gateway',
  503: 'Service Unavailable',
  504: 'Gateway Timeout',
};

// 语义色板（低饱和背景 + 深色前景），亮/暗双写。
const TONE = {
  emerald: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  sky: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  amber: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  rose: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  slate: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
} as const;

function statusTone(code: number): string {
  if (code >= 200 && code < 300) return TONE.emerald;
  if (code >= 300 && code < 400) return TONE.sky;
  if (code >= 400 && code < 500) return TONE.amber;
  if (code >= 500) return TONE.rose;
  return TONE.slate;
}

// HTTP 方法 → 语义色（GET 蓝 / POST 绿 / DELETE 红 / 其余中性）。
function methodTone(m?: string): string {
  switch ((m ?? '').toUpperCase()) {
    case 'GET':
      return TONE.sky;
    case 'POST':
      return TONE.emerald;
    case 'DELETE':
      return TONE.rose;
    default:
      return TONE.slate;
  }
}

const MONO = 'font-mono text-xs';

// 摘要栏中的小字段：图标 + 值。
function SummaryField({ icon, value, mono, title }: { icon: ReactNode; value: ReactNode; mono?: boolean; title?: string }) {
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5 text-xs text-slate-600 dark:text-slate-300">
      <span className="shrink-0 text-slate-400 dark:text-slate-500">{icon}</span>
      <span title={title} className={`truncate ${mono ? MONO : ''}`}>{value}</span>
    </span>
  );
}

// 详情卡片内的 key/value 行。
function InfoRow({ label, value, mono, title }: { label: string; value: ReactNode; mono?: boolean; title?: string }) {
  return (
    <div className="flex gap-3">
      <span className="w-24 shrink-0 text-xs text-slate-500 dark:text-slate-400">{label}</span>
      <span title={title} className={`min-w-0 flex-1 break-all text-slate-800 dark:text-slate-200 ${mono ? MONO : 'text-sm'}`}>
        {value}
      </span>
    </div>
  );
}

// 详情卡片：图标 + 标题 + definition list 风格内容。
function InfoCard({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <div className="rounded-th-card border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-800">
      <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-slate-700 dark:text-slate-200">
        <span className="text-slate-400 dark:text-slate-500">{icon}</span>
        {title}
      </div>
      <div className="space-y-2">{children}</div>
    </div>
  );
}

// 复制请求 ID 的小按钮；点击写入剪贴板并提示。
function CopyRequestIdButton({ value, label, className }: { value: string; label: string; className?: string }) {
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      toast.success(label);
    } catch {
      toast.error(label);
    }
  };
  return (
    <button
      type="button"
      onClick={copy}
      title={label}
      aria-label={label}
      className={className ?? 'shrink-0 rounded p-1 text-slate-400 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-500 dark:hover:bg-slate-700 dark:hover:text-slate-200'}
    >
      <Square2StackIcon className="h-4 w-4" />
    </button>
  );
}

const DASH = <span className="text-slate-400 dark:text-slate-500">—</span>;
const emptyIfBlank = (v?: string | null): ReactNode => (v && v.trim() ? v : DASH);
const orDash = (v?: string | number | null): ReactNode => (v === null || v === undefined || v === '' ? DASH : v);

const cardIcon = 'h-4 w-4';

export function ExpandedLogDetail({
  tab,
  row,
  colSpan,
}: {
  tab: LogTab;
  row: LogRow;
  colSpan: number;
}) {
  const { t } = useTranslation('admin');
  const [rawOpen, setRawOpen] = useState(false);

  const access = tab === 'access' ? (row as AccessLogRow) : null;
  const err = tab === 'error' ? (row as ErrorLogRow) : null;
  const audit = tab === 'audit' ? (row as AuditLogRow) : null;

  const status = access?.status ?? err?.status;
  const method = access?.method;
  const host = access?.host ?? err?.host;
  const path = access?.path ?? err?.path;
  const clientIp = access?.client_ip;
  const duration = access?.duration_ms;
  const requestId = access?.request_id ?? err?.request_id ?? audit?.id;
  const time = access?.timestamp ?? err?.timestamp ?? audit?.created_at;

  const timeStr = time ? new Date(time).toLocaleString() : '';

  // 请求/客户端两卡片对三类 Tab 均给出可读字段；缺失项以「—」占位，不伪造数据。
  return (
    <tr className="border-b border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40">
      <td colSpan={colSpan} className="px-4 py-4">
        <div className="space-y-3">
          {/* 第一层：请求摘要栏 */}
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-th-card border border-slate-200 bg-white px-4 py-3 dark:border-slate-700 dark:bg-slate-800">
            {status !== undefined && (
              <span className={`inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-semibold ${statusTone(status)}`}>
                {status}
                {STATUS_PHRASE[status] && <span className="font-normal opacity-80">{STATUS_PHRASE[status]}</span>}
              </span>
            )}
            {method && (
              <span className={`inline-flex shrink-0 items-center rounded px-2 py-0.5 text-xs font-semibold ${methodTone(method)}`}>
                {method}
              </span>
            )}
            {path && (
              <SummaryField icon={<DocumentTextIcon className="h-3.5 w-3.5" />} value={path} mono title={path} />
            )}
            {host && <SummaryField icon={<ServerIcon className="h-3.5 w-3.5" />} value={host} title={host} />}
            {clientIp && <SummaryField icon={<GlobeAltIcon className="h-3.5 w-3.5" />} value={clientIp} mono />}
            {duration !== undefined && (
              <SummaryField icon={<ClockIcon className="h-3.5 w-3.5" />} value={`${duration} ms`} />
            )}
            {requestId && (
              <span className="ml-auto inline-flex shrink-0 items-center gap-1">
                <CopyRequestIdButton value={requestId} label={t('logs.copiedRequestId')} />
                <button
                  type="button"
                  onClick={() => setRawOpen((v) => !v)}
                  className="rounded px-2 py-1 text-xs text-slate-500 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                >
                  {t('logs.viewRawLog')}
                </button>
              </span>
            )}
          </div>

          {/* 第二层：详情卡片 */}
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
            <InfoCard title={t('logs.requestInfo')} icon={<DocumentTextIcon className={cardIcon} />}>
              <InfoRow label={t('logs.time')} value={orDash(timeStr)} mono title={timeStr} />
              <InfoRow label={t('logs.host')} value={emptyIfBlank(host)} mono title={host} />
              <InfoRow label={t('logs.method')} value={emptyIfBlank(method)} mono />
              <InfoRow label={t('logs.path')} value={emptyIfBlank(path)} mono title={path} />
              <InfoRow label={t('logs.statusCode')} value={orDash(status)} mono />
              <InfoRow label={t('logs.duration')} value={duration === undefined ? DASH : `${duration} ms`} mono />
            </InfoCard>

            <InfoCard title={t('logs.clientInfo')} icon={<GlobeAltIcon className={cardIcon} />}>
              <InfoRow label={t('logs.clientIp')} value={emptyIfBlank(clientIp)} mono />
              {/* 后端当前未采集以下字段，固定以「—」占位，不伪造数据。 */}
              <InfoRow label={t('logs.userAgent')} value={DASH} mono />
              <InfoRow label={t('logs.xForwardedFor')} value={DASH} mono />
              <InfoRow label={t('logs.xRealIp')} value={DASH} mono />
            </InfoCard>

            <InfoCard title={t('logs.traceInfo')} icon={<ServerIcon className={cardIcon} />}>
              <div className="flex gap-3">
                <span className="w-24 shrink-0 text-xs text-slate-500 dark:text-slate-400">{t('logs.requestId')}</span>
                <span className="flex min-w-0 flex-1 items-center gap-1">
                  {requestId ? (
                    <>
                      <span title={requestId} className="min-w-0 flex-1 truncate rounded border border-slate-200 bg-slate-50 px-2 py-0.5 font-mono text-xs text-slate-700 dark:border-slate-700 dark:bg-slate-900/60 dark:text-slate-200">
                        {requestId}
                      </span>
                      <CopyRequestIdButton value={requestId} label={t('logs.copiedRequestId')} />
                    </>
                  ) : (
                    DASH
                  )}
                </span>
              </div>
              {/* 后端当前未提供以下追踪字段，固定以「—」占位。 */}
              <InfoRow label={t('logs.upstream')} value={DASH} />
              <InfoRow label={t('logs.routeName')} value={DASH} />
              <InfoRow label={t('logs.gatewayInstance')} value={DASH} />
              <InfoRow label={t('logs.node')} value={DASH} />
            </InfoCard>
          </div>

          {/* 第三层：原始日志（可折叠），内容为该行原始对象，做 pretty print。 */}
          {rawOpen && (
            <div className="rounded-th-card border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-800">
              <div className="flex items-center justify-between border-b border-slate-200 px-3 py-2 dark:border-slate-700">
                <button
                  type="button"
                  onClick={() => setRawOpen((v) => !v)}
                  aria-expanded={rawOpen}
                  className="inline-flex items-center gap-1 text-xs font-medium text-slate-600 hover:text-slate-800 dark:text-slate-300 dark:hover:text-slate-100"
                >
                  <ChevronDownIcon className="h-4 w-4" />
                  {t('logs.rawLog')}
                </button>
                <button
                  type="button"
                  onClick={async () => {
                    try {
                      await navigator.clipboard.writeText(JSON.stringify(row, null, 2));
                      toast.success(t('common.copied'));
                    } catch {
                      toast.error(t('common.operateFailed'));
                    }
                  }}
                  className="rounded px-2 py-0.5 text-xs text-slate-500 hover:bg-slate-100 hover:text-slate-700 dark:text-slate-400 dark:hover:bg-slate-700 dark:hover:text-slate-200"
                >
                  {t('common.copied')}
                </button>
              </div>
              <pre className="max-h-[300px] overflow-auto whitespace-pre-wrap break-all rounded-b-th-card bg-slate-50 p-3 font-mono text-xs text-slate-700 dark:bg-slate-900/60 dark:text-slate-200">
                {JSON.stringify(row, null, 2)}
              </pre>
            </div>
          )}
        </div>
      </td>
    </tr>
  );
}

// 展开/收起切换用的 chevron 图标。
export function ExpandChevron({ open }: { open: boolean }) {
  const Icon = open ? ChevronDownIcon : ChevronRightIcon;
  return <Icon className="h-4 w-4 text-slate-400 dark:text-slate-500" />;
}

// 表格状态码单元格：语义色文字，窄屏也不撑破布局。
export function StatusPill({ code }: { code?: number }) {
  if (code === undefined || code === null) return DASH;
  return <span className={`font-mono text-xs font-medium ${statusTone(code)} rounded px-1.5 py-0.5`}>{code}</span>;
}

