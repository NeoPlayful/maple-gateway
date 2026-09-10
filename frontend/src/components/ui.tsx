// 通用 UI 小组件：状态徽章、操作按钮、弹窗。
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

const color: Record<string, string> = {
  // 资源状态
  active: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  enabled: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  online: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  healthy: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  // 中间态
  pending: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  draining: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  recovering: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  paused: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  // 异常
  suspended: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  disabled: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  offline: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  maintenance: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  unknown: 'bg-slate-200 text-slate-600 dark:bg-slate-700 dark:text-slate-300',
  unhealthy: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  failed: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  rolled_back: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  // 证书状态（Direct TLS）
  error: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  expiring: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  expired: 'bg-rose-100 text-rose-700 dark:bg-rose-900/50 dark:text-rose-300',
  // 版本/发布状态
  stable: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  canary: 'bg-amber-100 text-amber-700 dark:bg-amber-900/50 dark:text-amber-300',
  standby: 'bg-sky-100 text-sky-700 dark:bg-sky-900/50 dark:text-sky-300',
  running: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/50 dark:text-emerald-300',
  created: 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300',
  completed: 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300',
};

// 状态文案取 i18n 词表 status.<key>；无词条则回退原始值（active/enabled 等保持英文枚举原样）。
export function StatusBadge({ value, raw }: { value: string; raw?: boolean }) {
  const { t } = useTranslation('admin');
  const key = value.toLowerCase();
  const cls = color[key] ?? 'bg-slate-100 text-slate-600 dark:bg-slate-800 dark:text-slate-300';
  const text = t(`status.${key}`, { defaultValue: raw ? value : value });
  return (
    <span className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${cls}`}>{text}</span>
  );
}

export function PhaseBadge({ phase }: { phase: string }) {
  return <StatusBadge value={phase} />;
}

export function ActionBtn({
  onClick,
  danger,
  children,
  disabled,
}: {
  onClick: () => void;
  danger?: boolean;
  children: React.ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`rounded px-2 py-1 text-xs disabled:opacity-40 ${
        danger
          ? 'bg-rose-600 text-white hover:bg-rose-700'
          : 'bg-slate-200 text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600'
      }`}
    >
      {children}
    </button>
  );
}

export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{label}</label>
      {children}
    </div>
  );
}

export const inputCls =
  'w-full rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';

// Modal 通用弹窗：遮罩 + 居中面板。打开时锁定背景滚动，Esc / 点击遮罩关闭。
export function Modal({
  open,
  title,
  onClose,
  children,
  footer,
  maxWidth = 'max-w-2xl',
}: {
  open: boolean;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  footer?: React.ReactNode;
  maxWidth?: string;
}) {
  const { t } = useTranslation('admin');

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => {
      document.removeEventListener('keydown', onKey);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/40 p-4 sm:items-center"
      onMouseDown={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        className={`w-full ${maxWidth} rounded-xl border border-slate-200 bg-white shadow-xl dark:border-slate-700 dark:bg-slate-800`}
        onMouseDown={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between border-b border-slate-200 px-5 py-3 dark:border-slate-700">
          <h2 className="text-sm font-semibold text-slate-700 dark:text-slate-100">{title}</h2>
          <button
            onClick={onClose}
            aria-label={t('common.close', '关闭')}
            className="rounded px-2 py-0.5 text-lg leading-none text-slate-400 hover:bg-slate-100 hover:text-slate-600 dark:hover:bg-slate-700 dark:hover:text-slate-200"
          >
            ×
          </button>
        </div>
        <div className="p-5">{children}</div>
        {footer && (
          <div className="flex justify-end gap-2 border-t border-slate-200 px-5 py-3 dark:border-slate-700">
            {footer}
          </div>
        )}
      </div>
    </div>
  );
}

// ConfirmDialog 通用确认弹窗：标题 + 提示语 + 取消/危险确认按钮。
// 复用 Modal 的遮罩、Esc 与滚动锁定；confirmLabel 默认"删除"，用于删除类危险操作。
export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel,
  onConfirm,
  onCancel,
  busy,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  onConfirm: () => void;
  onCancel: () => void;
  busy?: boolean;
}) {
  const { t } = useTranslation('admin');
  return (
    <Modal
      open={open}
      title={title}
      onClose={onCancel}
      maxWidth="max-w-sm"
      footer={
        <>
          <button
            onClick={onCancel}
            className="rounded bg-slate-200 px-4 py-1.5 text-sm text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600"
          >
            {t('common.cancel', '取消')}
          </button>
          <button
            onClick={onConfirm}
            disabled={busy}
            className="rounded bg-rose-600 px-4 py-1.5 text-sm text-white hover:bg-rose-700 disabled:opacity-50"
          >
            {confirmLabel ?? t('common.delete', '删除')}
          </button>
        </>
      }
    >
      <p className="text-sm text-slate-600 dark:text-slate-300">{message}</p>
    </Modal>
  );
}
