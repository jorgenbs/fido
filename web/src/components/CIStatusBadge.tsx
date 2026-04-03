interface CIStatusBadgeProps {
  status: string;
  url?: string;
}

const STATUS_STYLES: Record<string, string> = {
  passed: 'bg-green-900/40 text-green-400 border-green-800',
  merged: 'bg-purple-900/40 text-purple-400 border-purple-800',
  failed: 'bg-red-900/40 text-red-400 border-red-800',
  running: 'bg-yellow-900/40 text-yellow-400 border-yellow-800',
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
