import { Badge } from './ui/badge';

const stageColors: Record<string, string> = {
  scanned: 'bg-indigo-100 text-indigo-800 border-indigo-300 dark:bg-indigo-950 dark:text-indigo-300 dark:border-indigo-800',
  investigated: 'bg-amber-100 text-amber-800 border-amber-300 dark:bg-amber-950 dark:text-amber-300 dark:border-amber-800',
  fixed: 'bg-emerald-100 text-emerald-800 border-emerald-300 dark:bg-emerald-950 dark:text-emerald-300 dark:border-emerald-800',
};

interface StageIndicatorProps {
  stage: string;
}

export function StageIndicator({ stage }: StageIndicatorProps) {
  const colorClass = stageColors[stage] ?? 'bg-slate-100 text-slate-600 border-slate-300 dark:bg-slate-800 dark:text-slate-400 dark:border-slate-700';
  return (
    <Badge className={`text-xs font-medium border ${colorClass}`}>
      {stage}
    </Badge>
  );
}
