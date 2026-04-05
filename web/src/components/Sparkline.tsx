interface SparklineProps {
  data: { count: number }[];
  width?: number;
  height?: number;
  trend?: 'rising' | 'declining' | 'stable';
}

export function Sparkline({ data, width = 80, height = 20, trend }: SparklineProps) {
  if (!data || data.length < 2) {
    return <svg width={width} height={height} className="inline-block" />;
  }

  const counts = data.map(d => d.count);
  const max = Math.max(...counts, 1);
  const padding = 1;

  const points = counts.map((count, i) => {
    const x = padding + (i / (counts.length - 1)) * (width - 2 * padding);
    const y = padding + (1 - count / max) * (height - 2 * padding);
    return `${x},${y}`;
  });

  const strokeColor =
    trend === 'rising'
      ? 'stroke-red-400'
      : trend === 'declining'
        ? 'stroke-green-400'
        : 'stroke-muted-foreground';

  return (
    <svg width={width} height={height} className="inline-block align-middle">
      <polyline
        points={points.join(' ')}
        fill="none"
        className={strokeColor}
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}
