// 通用确认弹窗：标题 + 提示语 + 取消/危险确认按钮。
// 复用 Modal 的遮罩、Esc 与滚动锁定；confirmLabel 默认"删除"，用于删除类危险操作。
import { useTranslation } from 'react-i18next';
import { Modal } from './Modal';

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
