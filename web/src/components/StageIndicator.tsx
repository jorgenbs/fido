interface Props {
  stage: string;
}

const stageColors: Record<string, string> = {
  scanned: '#f59e0b',
  investigated: '#3b82f6',
  fixed: '#10b981',
};

export function StageIndicator({ stage }: Props) {
  return (
    <span
      style={{
        padding: '2px 8px',
        borderRadius: '4px',
        backgroundColor: stageColors[stage] || '#6b7280',
        color: 'white',
        fontSize: '0.85em',
        fontWeight: 500,
      }}
    >
      {stage}
    </span>
  );
}
