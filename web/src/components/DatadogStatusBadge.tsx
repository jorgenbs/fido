interface DatadogStatusBadgeProps {
  status: string;
  regressionCount: number;
}

const STATUS_STYLES: Record<string, string> = {
  resolved: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  regression: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
  acknowledged: 'bg-muted text-muted-foreground border-border',
  ignored: 'bg-muted text-muted-foreground border-border',
};

export function DatadogStatusBadge({ status, regressionCount }: DatadogStatusBadgeProps) {
  if (!status) return null;

  const isRegression = (status === 'open' || status === 'for_review') && regressionCount > 0;

  if (isRegression) {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES.regression}`;
    return <span className={classes}>Regression{regressionCount > 1 ? ` (${regressionCount})` : ''}</span>;
  }

  if (status === 'resolved') {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES.resolved}`;
    return <span className={classes}>Resolved</span>;
  }

  if (status === 'acknowledged' || status === 'ignored') {
    const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES[status]}`;
    return <span className={classes}>{status}</span>;
  }

  // open/for_review with no regressions — no badge (default state)
  return null;
}
