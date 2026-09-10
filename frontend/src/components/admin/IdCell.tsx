// 短 ID 单元格：显示前 8 位，悬停可见完整值，点击复制完整值。
import { useTranslation } from 'react-i18next';
import toast from 'react-hot-toast';
import { Square2StackIcon } from '@heroicons/react/24/outline';

export function IdCell({ id }: { id?: string | null }) {
  const { t } = useTranslation('admin');
  if (!id) {
    return <span className="text-slate-400 dark:text-slate-500">-</span>;
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(id);
      toast.success(t('common.copied', '已复制'));
    } catch {
      toast.error(t('common.operateFailed', '操作失败'));
    }
  };
  return (
    <span className="inline-flex items-center gap-1 font-mono text-xs text-slate-700 dark:text-slate-300">
      <span title={id}>{id.slice(0, 8)}</span>
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          copy();
        }}
        title={t('common.copyId', '复制 ID')}
        aria-label={t('common.copyId', '复制 ID')}
        className="text-slate-400 hover:text-slate-700 dark:text-slate-500 dark:hover:text-slate-200"
      >
        <Square2StackIcon className="h-3.5 w-3.5" />
      </button>
    </span>
  );
}
