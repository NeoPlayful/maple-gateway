// 详情网格：把一组 标签/值 以两列布局展示，用于列表行的展开详情。
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';

export function DetailGrid({
  items,
}: {
  items: { label: string; value?: ReactNode }[];
}) {
  const { t } = useTranslation('admin');
  return (
    <dl className="grid grid-cols-1 gap-x-8 gap-y-2 text-sm sm:grid-cols-2">
      {items.map((it) => (
        <div key={it.label} className="flex gap-2">
          <dt className="shrink-0 text-slate-500 dark:text-slate-400">{t(it.label)}</dt>
          <dd className="break-all font-mono text-xs text-slate-800 dark:text-slate-200">
            {it.value === null || it.value === undefined || it.value === '' ? '-' : it.value}
          </dd>
        </div>
      ))}
    </dl>
  );
}
