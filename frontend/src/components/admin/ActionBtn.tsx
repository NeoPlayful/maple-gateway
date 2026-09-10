// 行内操作按钮：默认次级样式，danger 为危险操作（删除等）。
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
