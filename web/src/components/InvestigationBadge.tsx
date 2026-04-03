const CONFIDENCE_STYLES: Record<string, string> = {
  high: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  medium: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  low: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
};

const COMPLEXITY_STYLES: Record<string, string> = {
  simple: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  moderate: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  complex: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
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
