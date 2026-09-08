// 轻量 SVG 折线图：趋势数据点渲染（无外部图表库）。
export interface Pt {
  value: number;
  label?: string;
}

export default function Sparkline({
  data,
  color = '#0d9488',
  height = 56,
  fill = true,
  suffix = '',
}: {
  data: Pt[];
  color?: string;
  height?: number;
  fill?: boolean;
  suffix?: string;
}) {
  const w = 100;
  if (data.length === 0) {
    return <div style={{ height }} className="flex items-center justify-center text-xs text-slate-300">暂无数据</div>;
  }
  const max = Math.max(...data.map((d) => d.value), 1);
  const step = data.length > 1 ? w / (data.length - 1) : w;
  const pts = data.map((d, i) => {
    const x = i * step;
    const y = height - (d.value / max) * (height - 4) - 2;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
  const last = data[data.length - 1];
  const areaPts = `0,${height} ${pts.join(' ')} ${w},${height}`;

  return (
    <div>
      <svg viewBox={`0 0 ${w} ${height}`} preserveAspectRatio="none" style={{ width: '100%', height, display: 'block' }}>
        {fill && (
          <polygon points={areaPts} fill={color} opacity={0.12} />
        )}
        <polyline
          points={pts.join(' ')}
          fill="none"
          stroke={color}
          strokeWidth={1.5}
          strokeLinejoin="round"
          strokeLinecap="round"
          vectorEffect="non-scaling-stroke"
        />
      </svg>
      <div className="mt-1 flex items-center justify-between text-xs text-slate-400">
        <span>{data.length} 个采样窗口</span>
        <span className="font-medium text-slate-600">{last.value}{suffix}</span>
      </div>
    </div>
  );
}
