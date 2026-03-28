import { Button } from './ui/button';

interface Props {
  onAllow: () => void;
  onDismiss: () => void;
}

export function NotificationBanner({ onAllow, onDismiss }: Props) {
  return (
    <div className="flex items-center justify-between px-4 py-2 bg-blue-950/30 border-b border-blue-900 text-xs">
      <span className="text-blue-300">
        Enable browser notifications to get alerted when issues change state or CI completes.
      </span>
      <div className="flex gap-2">
        <Button size="sm" className="h-6 text-xs" onClick={onAllow}>
          Enable
        </Button>
        <button className="text-muted-foreground hover:text-foreground text-xs" onClick={onDismiss}>
          Dismiss
        </button>
      </div>
    </div>
  );
}
