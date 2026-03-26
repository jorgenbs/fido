import { useEffect, useRef, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getIssue,
  triggerInvestigate,
  triggerFix,
  subscribeProgress,
  type IssueDetail as IssueDetailType,
} from '../api/client';
import { StageIndicator } from '../components/StageIndicator';
import { MarkdownViewer } from '../components/MarkdownViewer';
import { Button } from '../components/ui/button';
import { toggleTheme } from '../lib/theme';

type RunningState = 'idle' | 'running' | 'error';

export function IssueDetail() {
  const { id } = useParams<{ id: string }>();
  const [issue, setIssue] = useState<IssueDetailType | null>(null);
  const [loading, setLoading] = useState(true);
  const [investigateState, setInvestigateState] = useState<RunningState>('idle');
  const [fixState, setFixState] = useState<RunningState>('idle');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const sseRef = useRef<EventSource | null>(null);

  const fetchIssue = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const data = await getIssue(id);
      setIssue(data);
    } catch (err) {
      console.error('Failed to fetch issue:', err);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchIssue();
    return () => sseRef.current?.close();
  }, [id]);

  const startSSE = (onComplete: () => void) => {
    if (!id) return;
    sseRef.current?.close();
    sseRef.current = subscribeProgress(id, (data) => {
      if (data.status === 'complete') {
        sseRef.current?.close();
        onComplete();
      } else if (data.status === 'error') {
        sseRef.current?.close();
        setErrorMsg(data.message ?? 'Unknown error');
        setInvestigateState('error');
        setFixState('idle');
      }
    });
  };

  const handleInvestigate = async () => {
    if (!id) return;
    setErrorMsg(null);
    setInvestigateState('running');
    try {
      await triggerInvestigate(id);
      startSSE(() => {
        setInvestigateState('idle');
        fetchIssue();
      });
    } catch (err) {
      setInvestigateState('error');
      setErrorMsg(String(err));
    }
  };

  const handleFix = async () => {
    if (!id) return;
    setErrorMsg(null);
    setFixState('running');
    try {
      await triggerFix(id);
      startSSE(() => {
        setFixState('idle');
        fetchIssue();
      });
    } catch (err) {
      setFixState('error');
      setErrorMsg(String(err));
    }
  };

  if (loading) return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
      <p className="text-muted-foreground text-sm">Loading…</p>
    </div>
  );
  if (!issue) return (
    <div className="min-h-screen bg-background text-foreground flex items-center justify-center">
      <p className="text-muted-foreground text-sm">Issue not found</p>
    </div>
  );

  return (
    <div className="min-h-screen bg-background text-foreground">
      {/* Nav */}
      <div className="flex justify-between items-center px-4 py-3 border-b border-border">
        <span className="font-bold text-base">🐕 fido</span>
        <button
          onClick={toggleTheme}
          className="text-xs text-muted-foreground hover:text-foreground transition-colors"
        >
          ☀ / 🌙
        </button>
      </div>

      <div className="max-w-4xl mx-auto px-4 py-6">
        <Link to="/" className="text-xs text-muted-foreground hover:text-foreground">
          ← Issues
        </Link>

        <div className="flex items-center gap-3 mt-4 mb-1">
          <h1 className="text-lg font-semibold">{issue.id}</h1>
          <StageIndicator stage={issue.stage} />
        </div>

        {/* Error message */}
        {errorMsg && (
          <div className="mb-4 p-3 rounded-md border border-red-800 bg-red-950/30 text-red-400 text-xs">
            {errorMsg}
          </div>
        )}

        {/* Error Report */}
        <Section title="Error Report">
          <MarkdownViewer title="" content={issue.error} />
        </Section>

        {/* Investigation */}
        <Section
          title="Investigation"
          running={investigateState === 'running'}
          runningLabel="Claude is analysing the codebase…"
        >
          {issue.investigation ? (
            <MarkdownViewer title="" content={issue.investigation} />
          ) : investigateState !== 'running' ? (
            <div className="p-4">
              <Button size="sm" onClick={handleInvestigate}>
                Investigate this issue
              </Button>
            </div>
          ) : null}
        </Section>

        {/* Fix */}
        <Section
          title="Fix"
          running={fixState === 'running'}
          runningLabel="Claude is implementing the fix…"
          disabled={!issue.investigation && fixState !== 'running'}
        >
          {issue.fix ? (
            <MarkdownViewer title="" content={issue.fix} />
          ) : issue.investigation && fixState !== 'running' ? (
            <div className="p-4">
              <Button size="sm" onClick={handleFix}>
                Fix this issue
              </Button>
            </div>
          ) : null}
        </Section>

        {/* Resolution */}
        {issue.resolve && (
          <Section title="Resolution">
            <div className="p-4 space-y-1 text-sm">
              <p><span className="text-muted-foreground">Branch:</span> <code className="text-xs">{issue.resolve.branch}</code></p>
              <p>
                <span className="text-muted-foreground">MR:</span>{' '}
                <a href={issue.resolve.mr_url} target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
                  {issue.resolve.mr_url}
                </a>
              </p>
              <p><span className="text-muted-foreground">Status:</span> {issue.resolve.mr_status}</p>
              <p><span className="text-muted-foreground">Created:</span> {new Date(issue.resolve.created_at).toLocaleString()}</p>
            </div>
          </Section>
        )}
      </div>
    </div>
  );
}

interface SectionProps {
  title: string;
  children: React.ReactNode;
  running?: boolean;
  runningLabel?: string;
  disabled?: boolean;
}

function Section({ title, children, running, runningLabel, disabled }: SectionProps) {
  return (
    <div className={`border rounded-md mb-4 ${disabled ? 'opacity-40 pointer-events-none' : ''} ${running ? 'border-blue-800 bg-blue-950/20' : 'border-border'}`}>
      <div className="flex justify-between items-center px-4 py-2.5 border-b border-inherit">
        <span className={`font-semibold text-sm ${running ? 'text-blue-400' : ''}`}>{title}</span>
        {running && (
          <span className="flex items-center gap-1.5 text-xs text-blue-400">
            <span className="inline-block w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
            {runningLabel ?? 'Running…'}
          </span>
        )}
      </div>
      {children}
    </div>
  );
}
