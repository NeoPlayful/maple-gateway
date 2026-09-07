export default function PlaceholderPage({ title }: { title: string }) {
  return (
    <div className="rounded-xl bg-white p-8 shadow-sm">
      <h1 className="mb-2 text-lg font-semibold text-slate-800">{title}</h1>
      <p className="text-sm text-slate-400">功能建设中，后续阶段填充。</p>
    </div>
  );
}
