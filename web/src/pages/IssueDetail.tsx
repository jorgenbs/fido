import { useEffect, useRef, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  getIssue,
  triggerInvestigate,
  triggerFix,
  subscribeProgress,
  fetchMRStatus,
  type IssueDetail as IssueDetailType,
} from '../api/client';
import { StageIndicator } from '../components/StageIndicator';
import { CIStatusBadge } from '../components/CIStatusBadge';
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
  const [progressLog, setProgressLog] = useState<string>('');
  const sseRef = useRef<EventSource | null>(null);
  const copyTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [copied, setCopied] = useState(false);

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
    return () => {
      sseRef.current?.close();
      if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current);
    };
  }, [id]);

  useEffect(() => {
    if (!id || !issue?.resolve) return;
    const ciTerminal = ['passed', 'failed', 'canceled'];
    const mrTerminal = ['merged', 'closed'];
    if (
      ciTerminal.includes(issue.ci_status) &&
      mrTerminal.includes(issue.resolve.mr_status ?? '')
    )
      return;

    const interval = setInterval(async () => {
      try {
        const data = await fetchMRStatus(id);
        if (
          data.ci_status !== issue.ci_status ||
          data.mr_status !== issue.resolve?.mr_status
        ) {
          fetchIssue();
        }
      } catch {
        // non-fatal: polling errors are ignored
      }
    }, 30_000);

    return () => clearInterval(interval);
  }, [id, issue?.ci_status, issue?.resolve?.mr_status]);

  const startSSE = (onComplete: () => void) => {
    if (!id) return;
    sseRef.current?.close();
    setProgressLog('');
    sseRef.current = subscribeProgress(id, (data) => {
      if (data.log) {
        setProgressLog((prev) => prev + data.log);
      }
      if (data.status === 'complete') {
        sseRef.current?.close();
        setProgressLog('');
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

  const handleRefix = async () => {
    if (!id) return;
    setErrorMsg(null);
    setFixState('running');
    try {
      await triggerFix(id, true);
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
          <MarkdownViewer content={issue.error} />
        </Section>

        {/* Investigation */}
        <Section
          title="Investigation"
          running={investigateState === 'running'}
          runningLabel="Claude is analysing the codebase…"
        >
          {issue.investigation ? (
            <MarkdownViewer content={issue.investigation} />
          ) : investigateState === 'running' ? (
            progressLog ? (
              <pre className="p-4 text-xs font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-96">
                {progressLog}
              </pre>
            ) : null
          ) : (
            <div className="p-4">
              <Button size="sm" onClick={handleInvestigate}>
                Investigate this issue
              </Button>
            </div>
          )}
        </Section>

        {/* Fix */}
        <Section
          title="Fix"
          running={fixState === 'running'}
          runningLabel="Claude is implementing the fix…"
          disabled={!issue.investigation && fixState !== 'running'}
        >
          {issue.fix ? (
            <MarkdownViewer content={issue.fix} />
          ) : fixState === 'running' ? (
            progressLog ? (
              <pre className="p-4 text-xs font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-96">
                {progressLog}
              </pre>
            ) : null
          ) : issue.investigation ? (
            <div className="p-4 space-y-2">
              <p className="text-xs text-muted-foreground">Run this command to fix the issue:</p>
              <div className="flex items-center gap-2 bg-muted/50 rounded px-3 py-2">
                <code className="flex-1 text-xs font-mono text-foreground">fido fix {issue.id}</code>
                <button
                  onClick={() => {
                    navigator.clipboard.writeText(`fido fix ${issue.id}`);
                    setCopied(true);
                    if (copyTimeoutRef.current) clearTimeout(copyTimeoutRef.current);
                    copyTimeoutRef.current = setTimeout(() => setCopied(false), 2000);
                  }}
                  className="text-xs text-muted-foreground hover:text-foreground shrink-0"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            </div>
          ) : null}
        </Section>

        {/* Resolution */}
        {issue.resolve && (
          <Section
            title="Resolution"
            running={issue.ci_status === 'running' || issue.ci_status === 'pending'}
            runningLabel="CI pipeline running…"
          >
            <div className="p-4 space-y-2 text-sm">
              <p><span className="text-muted-foreground">Branch:</span> <code className="text-xs">{issue.resolve.branch}</code></p>
              <p>
                <span className="text-muted-foreground">MR:</span>{' '}
                <a href={issue.resolve.mr_url} target="_blank" rel="noreferrer" className="text-blue-400 hover:underline">
                  {issue.resolve.mr_url}
                </a>
              </p>
              <p><span className="text-muted-foreground">MR Status:</span> {issue.resolve.mr_status}</p>
              {issue.ci_status && (
                <p className="flex items-center gap-2">
                  <span className="text-muted-foreground">CI:</span>
                  <CIStatusBadge status={issue.ci_status} url={issue.ci_url || undefined} />
                </p>
              )}
              <p><span className="text-muted-foreground">Created:</span> {new Date(issue.resolve.created_at).toLocaleString()}</p>
              {issue.stage === 'fixed' && issue.ci_status === 'failed' && fixState !== 'running' && (
                <div className="pt-2">
                  <Button size="sm" variant="outline" onClick={handleRefix} className="border-red-800 text-red-400 hover:bg-red-950/30">
                    Re-fix (CI failing)
                  </Button>
                </div>
              )}
              {fixState === 'running' && progressLog && (
                <pre className="mt-2 p-4 text-xs font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-96">
                  {progressLog}
                </pre>
              )}
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
