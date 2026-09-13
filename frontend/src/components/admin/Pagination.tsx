// 通用分页条：上一页/下一页 + 页码信息，供各类内联小表复用。
// 数据量不超过一页时不渲染（调用方无需自行判断）。
import { useTranslation } from 'react-i18next';
import { ChevronLeftIcon, ChevronRightIcon } from '@heroicons/react/24/outline';

export function Pagination({
  page,
  pageSize,
  total,
  onChange,
}: {
  page: number; // 1-based
  pageSize: number;
  total: number;
  onChange: (page: number) => void;
}) {
  const { t } = useTranslation('admin');
  const totalPages = Math.max(1, Math.ceil(total / pageSize));
  if (total <= pageSize) return null;

  const cur = Math.min(Math.max(page, 1), totalPages);
  const btn =
    'inline-flex items-center gap-0.5 rounded px-2 py-1 text-xs bg-slate-200 text-slate-700 hover:bg-slate-300 dark:bg-slate-700 dark:text-slate-200 dark:hover:bg-slate-600 disabled:opacity-40 disabled:cursor-not-allowed';

  return (
    <div className="mt-1 flex items-center justify-end gap-2 text-xs text-slate-500 dark:text-slate-400">
      <button className={btn} disabled={cur <= 1} onClick={() => onChange(cur - 1)}>
        <ChevronLeftIcon className="h-3.5 w-3.5" />
        {t('common.prev')}
      </button>
      <span>{t('common.pageInfo', { page: cur, total: totalPages })}</span>
      <button className={btn} disabled={cur >= totalPages} onClick={() => onChange(cur + 1)}>
        {t('common.next')}
        <ChevronRightIcon className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}
