// 通用弹窗：遮罩 + 居中面板。打开时锁定背景滚动，Esc / 点击遮罩关闭。
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';

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
