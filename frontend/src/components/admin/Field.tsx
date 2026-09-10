// 表单字段：标签 + 控件的纵向布局。
export function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <label className="mb-1 block text-xs text-slate-500 dark:text-slate-400">{label}</label>
      {children}
    </div>
  );
}
