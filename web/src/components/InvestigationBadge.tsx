const CONFIDENCE_STYLES: Record<string, string> = {
  high: 'bg-green-900/40 text-green-400 border-green-800',
  medium: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
  low: 'bg-red-900/40 text-red-400 border-red-800',
};

const COMPLEXITY_STYLES: Record<string, string> = {
  simple: 'bg-green-900/40 text-green-400 border-green-800',
  moderate: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
  complex: 'bg-red-900/40 text-red-400 border-red-800',
};

interface InvestigationBadgeProps {
  type: 'confidence' | 'complexity';
  value: string;
}

export function InvestigationBadge({ type, value }: InvestigationBadgeProps) {
  if (!value) return <span className="text-muted-foreground text-xs">—</span>;

  const styles = type === 'confidence' ? CONFIDENCE_STYLES : COMPLEXITY_STYLES;
  const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${
    styles[value.toLowerCase()] ?? 'bg-muted text-muted-foreground border-border'
  }`;

  return <span className={classes}>{value}</span>;
}
