import { Badge } from './ui/badge';

const stageColors: Record<string, string> = {
  scanned: 'bg-indigo-950 text-indigo-300 border-indigo-800',
  investigated: 'bg-amber-950 text-amber-300 border-amber-800',
  fixed: 'bg-emerald-950 text-emerald-300 border-emerald-800',
};

interface StageIndicatorProps {
  stage: string;
}

export function StageIndicator({ stage }: StageIndicatorProps) {
  const colorClass = stageColors[stage] ?? 'bg-slate-800 text-slate-400 border-slate-700';
  return (
    <Badge className={`text-xs font-medium border ${colorClass}`}>
      {stage}
    </Badge>
  );
}
