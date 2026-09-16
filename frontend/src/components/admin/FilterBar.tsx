// 通用筛选栏：给列表页提供统一的单行筛选 UI + 前端过滤纯函数。
// CrudPage 与各手写列表页共用，避免每个页面各写一套筛选实现。
import { useTranslation } from 'react-i18next';

// 筛选项定义：keyword 跨字段模糊匹配；select 按行字段精确匹配。
// select 候选项来源优先级：静态 options → 动态 loadOptions（从 parents 取）→ 按行数据去重派生。
export interface FilterDef {
  type: 'keyword' | 'select';
  key: string; // keyword 只用其一；select 为行字段名（如 status / tenant_id / service_id）
  keys?: string[]; // keyword：参与匹配的行字段（取字符串拼接后模糊匹配）
  placeholder?: string; // 空选项/占位文案 i18n key（keyword 为输入框 placeholder，select 为"全部"选项）
  options?: { value: string; label: string }[]; // select：静态选项
  loadOptions?: { labelKey: string; valueKey: string; path: string }; // select：动态选项来源
  labelPrefix?: string; // select：选项文案 i18n 前缀（如 status → status.active）
}

// 筛选栏控件样式：不含 w-full（否则 Tailwind 中 w-full 会压过固定宽度导致整条栏溢出屏幕），
// 宽度由调用点显式给出。
const filterCls =
  'rounded border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-800 placeholder-slate-400 focus:border-th-accent-focus focus:outline-none dark:border-slate-600 dark:bg-slate-800 dark:text-slate-200 dark:placeholder-slate-500';

// applyFilters 按筛选项过滤行：keyword 跨 keys 模糊匹配（大小写不敏感）；select 按行字段精确匹配；多条件 AND。
export function applyFilters<T extends Record<string, any>>(
  rows: T[],
  filters: FilterDef[],
  values: Record<string, string>,
): T[] {
  if (filters.length === 0) return rows;
  return rows.filter((r) =>
    filters.every((f) => {
      const v = values[f.key] ?? '';
      if (!v) return true;
      if (f.type === 'select') return String(r[f.key] ?? '') === v;
      const hay = (f.keys ?? [f.key]).map((k) => String(r[k] ?? '')).join(' ').toLowerCase();
      return hay.includes(v.trim().toLowerCase());
    }),
  );
}

export function hasActiveFilter(filters: FilterDef[], values: Record<string, string>): boolean {
  return filters.some((f) => (values[f.key] ?? '') !== '');
}

export function FilterBar({
  filters,
  values,
  onChange,
  onReset,
  rows,
  parents,
}: {
  filters: FilterDef[];
  values: Record<string, string>;
  onChange: (key: string, value: string) => void;
  onReset: () => void;
  rows: Record<string, any>[]; // select 无固定选项时按行数据去重派生
  parents?: Record<string, any[]>; // select 动态选项来源（与表单字段共用的预取缓存）
}) {
  const { t } = useTranslation('admin');
  if (filters.length === 0) return null;

  // 解析 select 候选项：静态 options → 动态 loadOptions → 行数据去重派生（labelPrefix 走前缀本地化）。
  const opts = (f: FilterDef): { value: string; label: string }[] => {
    if (f.options) return f.options.map((o) => ({ value: o.value, label: t(o.label, { defaultValue: o.label }) }));
    if (f.loadOptions) {
      const src = parents?.[f.loadOptions.path] ?? [];
      return src.map((p) => {
        const label = String(p[f.loadOptions!.labelKey] ?? '');
        return { value: String(p[f.loadOptions!.valueKey]), label: t(label, { defaultValue: label }) };
      });
    }
    const seen = new Set<string>();
    const out: { value: string; label: string }[] = [];
    for (const r of rows) {
      const v = r[f.key];
      if (v === null || v === undefined || v === '') continue;
      const s = String(v);
      if (seen.has(s)) continue;
      seen.add(s);
      out.push({ value: s, label: f.labelPrefix ? t(`${f.labelPrefix}.${s}`, { defaultValue: s }) : s });
    }
    return out.sort((a, b) => a.label.localeCompare(b.label));
  };

  return (
    <div className="mb-3 overflow-x-auto">
      <div className="flex items-center gap-3">
        {filters.map((f) =>
          f.type === 'keyword' ? (
            <input
              key={f.key}
              value={values[f.key] ?? ''}
              onChange={(e) => onChange(f.key, e.target.value)}
              placeholder={f.placeholder ? t(f.placeholder) : t('common.search', '搜索')}
              className={`${filterCls} w-48 shrink-0`}
            />
          ) : (
            <select
              key={f.key}
              value={values[f.key] ?? ''}
              onChange={(e) => onChange(f.key, e.target.value)}
              className={`${filterCls} w-40 shrink-0`}
            >
              <option value="">
                {f.placeholder ? t(f.placeholder) : t('common.emptyPlaceholder', '留空全部')}
              </option>
              {opts(f).map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </select>
          ),
        )}
        {hasActiveFilter(filters, values) && (
          <button
            onClick={onReset}
            className="shrink-0 rounded bg-slate-100 px-3 py-1.5 text-sm text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600"
          >
            {t('common.resetFilter', '重置')}
          </button>
        )}
      </div>
    </div>
  );
}
