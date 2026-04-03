interface CIStatusBadgeProps {
  status: string;
  url?: string;
}

const STATUS_STYLES: Record<string, string> = {
  passed: 'bg-green-100 text-green-800 border-green-300 dark:bg-green-900/40 dark:text-green-400 dark:border-green-800',
  merged: 'bg-purple-100 text-purple-800 border-purple-300 dark:bg-purple-900/40 dark:text-purple-400 dark:border-purple-800',
  failed: 'bg-red-100 text-red-800 border-red-300 dark:bg-red-900/40 dark:text-red-400 dark:border-red-800',
  running: 'bg-yellow-100 text-yellow-800 border-yellow-300 dark:bg-yellow-900/40 dark:text-yellow-400 dark:border-yellow-800',
  pending: 'bg-muted text-muted-foreground border-border',
  canceled: 'bg-muted text-muted-foreground border-border',
};

export function CIStatusBadge({ status, url }: CIStatusBadgeProps) {
  if (!status) return <span className="text-muted-foreground text-xs">—</span>;

  const classes = `inline-block border rounded px-1.5 py-0.5 text-xs font-medium ${STATUS_STYLES[status] ?? 'bg-muted text-muted-foreground border-border'}`;

  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer" className={classes} onClick={(e) => e.stopPropagation()}>
        {status}
      </a>
    );
  }
  return <span className={classes}>{status}</span>;
}
