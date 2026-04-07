import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import {
  listIssues,
  triggerScan,
  triggerInvestigate as apiInvestigate,
  ignoreIssue,
  unignoreIssue,
  type IssueListItem,
  type SSEEvent,
} from '../api/client';
import { useEventStream } from '../hooks/useEventStream';
import { useNotifications } from '../hooks/useNotifications';
import { NotificationBanner } from '../components/NotificationBanner';
import { StageIndicator } from '../components/StageIndicator';
import { CIStatusBadge } from '../components/CIStatusBadge';
import { InvestigationBadge } from '../components/InvestigationBadge';
import { Button } from '../components/ui/button';
import { Checkbox } from '../components/ui/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select';
import { toggleTheme } from '../lib/theme';

export function Dashboard() {
  const [issues, setIssues] = useState<IssueListItem[]>([]);
  const [filter, setFilter] = useState('all');
  const [showIgnored, setShowIgnored] = useState(false);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const navigate = useNavigate();
  const lastSelectedRef = useRef<string | null>(null);
  const [serviceFilter, setServiceFilter] = useState('all');
  const [confidenceFilter, setConfidenceFilter] = useState('all');
  const [complexityFilter, setComplexityFilter] = useState('all');
  const [codeFixableOnly, setCodeFixableOnly] = useState(false);
  const [highlightedIds, setHighlightedIds] = useState<Set<string>>(new Set());
  const [actionLoading, setActionLoading] = useState<Record<string, string>>({});

  const { permission, requestPermission, notify } = useNotifications();
  const [bannerDismissed, setBannerDismissed] = useState(() =>
    localStorage.getItem('fido:notif-dismissed') === 'true'
  );
  const showBanner = permission === 'default' && !bannerDismissed;

  const dismissBanner = () => {
    setBannerDismissed(true);
    localStorage.setItem('fido:notif-dismissed', 'true');
  };

  const fetchIssues = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    try {
      const data = await listIssues(filter === 'all' ? undefined : filter, showIgnored);
      setIssues(data);
      if (!silent) setSelectedIds(new Set());
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    } finally {
      if (!silent) setLoading(false);
    }
  }, [filter, showIgnored]);

  useEffect(() => {
    fetchIssues();
  }, [fetchIssues]);

  useEventStream((event: SSEEvent) => {
    const id = event.payload?.id as string | undefined;

    switch (event.type) {
      case 'scan:complete': {
        const count = event.payload?.count as number;
        fetchIssues(true);
        if (count > 0) {
          notify('New issues found', { body: `Scan discovered ${count} new issue${count === 1 ? '' : 's'}` });
        }
        break;
      }
      case 'issue:updated': {
        const field = event.payload?.field as string;
        const newValue = event.payload?.newValue;
        if (id) {
          fetchIssues(true);
          setHighlightedIds(prev => new Set(prev).add(id));
          setTimeout(() => {
            setHighlightedIds(prev => {
              const next = new Set(prev);
              next.delete(id);
              return next;
            });
          }, 3000);

          const goToIssue = () => navigate(`/issues/${id}`);
          if (field === 'stage' && newValue === 'investigated') {
            notify('Investigation complete', { body: `Issue ${id} has been investigated`, onClick: goToIssue });
          } else if (field === 'stage' && newValue === 'fixed') {
            notify('Fix applied', { body: `Issue ${id} has been fixed`, onClick: goToIssue });
          } else if (field === 'ci_status' && newValue === 'passed') {
            notify('CI passed', { body: `Issue ${id}: pipeline passed`, onClick: goToIssue });
          } else if (field === 'ci_status' && newValue === 'failed') {
            notify('CI failed', { body: `Issue ${id}: pipeline failed`, onClick: goToIssue });
          }
        }
        break;
      }
      case 'issue:progress':
        if (id) {
          setIssues(prev => prev.map(issue =>
            issue.id === id
              ? { ...issue, running_op: event.payload.status === 'started' ? event.payload.action as 'investigate' | 'fix' : undefined }
              : issue
          ));
          if (event.payload.status === 'complete') {
            fetchIssues(true);
            setHighlightedIds(prev => new Set(prev).add(id));
            setTimeout(() => {
              setHighlightedIds(prev => {
                const next = new Set(prev);
                next.delete(id);
                return next;
              });
            }, 3000);
          }
        }
        break;
    }
  });

  const handleScan = async () => {
    await triggerScan();
    await fetchIssues();
  };

  const handleIgnore = async (id: string, currentlyIgnored: boolean) => {
    try {
      if (currentlyIgnored) {
        await unignoreIssue(id);
      } else {
        await ignoreIssue(id);
      }
      await fetchIssues();
    } catch (err) {
      console.error('Failed to toggle ignore:', err);
    }
  };

  const handleInvestigate = async (id: string) => {
    setActionLoading(prev => ({ ...prev, [id]: 'investigate' }));
    try {
      await apiInvestigate(id);
    } catch (err) {
      console.error('Failed to trigger investigate:', err);
    } finally {
      setActionLoading(prev => {
        const next = { ...prev };
        delete next[id];
        return next;
      });
    }
  };


  const handleBulkIgnore = async () => {
    const toIgnore = issues.filter(i => selectedIds.has(i.id) && !i.ignored);
    await Promise.all(toIgnore.map(i => ignoreIssue(i.id)));
    await fetchIssues();
  };

  const handleBulkUnignore = async () => {
    const toUnignore = issues.filter(i => selectedIds.has(i.id) && i.ignored);
    await Promise.all(toUnignore.map(i => unignoreIssue(i.id)));
    await fetchIssues();
  };

  const toggleRow = (id: string) => {
    setExpandedId(expandedId === id ? null : id);
  };

  const services = [...new Set(issues.map(i => i.service).filter(Boolean))].sort();

  const filteredIssues = issues.filter(issue => {
    if (serviceFilter !== 'all' && issue.service !== serviceFilter) return false;
    if (confidenceFilter !== 'all' && issue.confidence.toLowerCase() !== confidenceFilter.toLowerCase()) return false;
    if (complexityFilter !== 'all' && issue.complexity.toLowerCase() !== complexityFilter.toLowerCase()) return false;
    if (codeFixableOnly && issue.code_fixable !== 'Yes') return false;
    return true;
  });

  const allSelected = filteredIssues.length > 0 && filteredIssues.every(i => selectedIds.has(i.id));
  const someSelected = selectedIds.size > 0;

  const toggleSelectAll = () => {
    lastSelectedRef.current = null;
    if (allSelected) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(filteredIssues.map(i => i.id)));
    }
  };

  const toggleSelectOne = (id: string, e: React.MouseEvent) => {
    const anchor = lastSelectedRef.current;
    if (e.shiftKey && anchor && anchor !== id) {
      const anchorIdx = filteredIssues.findIndex(i => i.id === anchor);
      const clickIdx = filteredIssues.findIndex(i => i.id === id);
      if (anchorIdx !== -1 && clickIdx !== -1) {
        const [min, max] = anchorIdx < clickIdx
          ? [anchorIdx, clickIdx]
          : [clickIdx, anchorIdx];
        setSelectedIds(prev => {
          const next = new Set(prev);
          filteredIssues.slice(min, max + 1).forEach(i => next.add(i.id));
          return next;
        });
        return;
      }
    }
    // Plain toggle
    lastSelectedRef.current = id;
    setSelectedIds(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

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

      {showBanner && (
        <NotificationBanner onAllow={requestPermission} onDismiss={dismissBanner} />
      )}

      {/* Toolbar */}
      <div className="flex justify-between items-center px-4 py-3 border-b border-border">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-sm">Issues</span>
          <span className="bg-muted text-muted-foreground rounded-full px-2 py-0.5 text-xs">
            {filteredIssues.length === issues.length
              ? issues.length
              : `${filteredIssues.length} / ${issues.length}`}
          </span>
        </div>
        <div className="flex items-center gap-3">
          <Select value={filter} onValueChange={setFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All stages" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All stages</SelectItem>
              <SelectItem value="scanned">Scanned</SelectItem>
              <SelectItem value="investigated">Investigated</SelectItem>
              <SelectItem value="fixed">Fixed</SelectItem>
            </SelectContent>
          </Select>
          {services.length > 0 && (
            <Select value={serviceFilter} onValueChange={setServiceFilter}>
              <SelectTrigger className="w-36 h-7 text-xs">
                <SelectValue placeholder="All services" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All services</SelectItem>
                {services.map(svc => (
                  <SelectItem key={svc} value={svc}>{svc}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Select value={confidenceFilter} onValueChange={setConfidenceFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All confidence" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All confidence</SelectItem>
              <SelectItem value="High">High</SelectItem>
              <SelectItem value="Medium">Medium</SelectItem>
              <SelectItem value="Low">Low</SelectItem>
            </SelectContent>
          </Select>
          <Select value={complexityFilter} onValueChange={setComplexityFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All complexity" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All complexity</SelectItem>
              <SelectItem value="Simple">Simple</SelectItem>
              <SelectItem value="Moderate">Moderate</SelectItem>
              <SelectItem value="Complex">Complex</SelectItem>
            </SelectContent>
          </Select>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
            <Checkbox
              checked={codeFixableOnly}
              onCheckedChange={(v) => setCodeFixableOnly(!!v)}
              className="w-3.5 h-3.5"
            />
            Code fixable only
          </label>
          <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
            <Checkbox
              checked={showIgnored}
              onCheckedChange={(v) => setShowIgnored(!!v)}
              className="w-3.5 h-3.5"
            />
            Show ignored
          </label>
          <Button size="sm" onClick={handleScan} className="h-7 text-xs">
            Scan Now
          </Button>
        </div>
      </div>

      <div className={`flex items-center gap-3 px-4 border-b text-xs h-8 ${someSelected ? "bg-blue-950/30 border-blue-900" : "border-transparent"}`}>
        <span className={`font-medium ${someSelected ? "text-blue-300" : "invisible"}`}>
          {someSelected ? `${selectedIds.size} selected` : "0 selected"}
        </span>
        {someSelected && (
          <>
            <Button size="sm" variant="outline" className="h-6 text-xs" onClick={handleBulkIgnore}>
              Ignore
            </Button>
            <Button size="sm" variant="outline" className="h-6 text-xs" onClick={handleBulkUnignore}>
              Unignore
            </Button>
            <button
              className="ml-auto text-muted-foreground hover:text-foreground text-xs"
              onClick={() => setSelectedIds(new Set())}
            >
              Clear selection
            </button>
          </>
        )}
      </div>

      {/* Table */}
      {loading ? (
        <p className="p-4 text-sm text-muted-foreground">Loading…</p>
      ) : issues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues found. Run a scan to get started.</p>
      ) : filteredIssues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues match the current filters.</p>
      ) : (
        <div>
          {/* Header row */}
          <div className="grid grid-cols-[32px_2fr_1fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr] px-4 py-2 bg-muted/50 text-xs font-semibold text-muted-foreground tracking-wide uppercase border-b border-border">
            <span>
              <Checkbox
                checked={allSelected}
                onCheckedChange={toggleSelectAll}
                className="w-3.5 h-3.5"
                onClick={(e: React.MouseEvent) => e.stopPropagation()}
              />
            </span>
            <span>Issue</span>
            <span>Service</span>
            <span>Stage</span>
            <span>Confidence</span>
            <span>Complexity</span>
            <span>Fixable</span>
            <span>CI</span>
            <span>MR</span>
          </div>

          {filteredIssues.map((issue) => (
            <div key={issue.id} className="border-b border-border">
              {/* Main row */}
              <div
                className={`grid grid-cols-[32px_2fr_1fr_0.6fr_0.6fr_0.5fr_0.5fr_0.6fr_0.6fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-all duration-500 ${selectedIds.has(issue.id) ? 'bg-blue-950/30' : ''} ${highlightedIds.has(issue.id) ? 'bg-yellow-500/20 ring-1 ring-yellow-500/30' : ''}`}
                onClick={() => toggleRow(issue.id)}
              >
                <span
                  onClick={(e) => {
                    e.stopPropagation();
                    toggleSelectOne(issue.id, e);
                  }}
                >
                  <Checkbox
                    checked={selectedIds.has(issue.id)}
                    onCheckedChange={() => {}}
                    className="w-3.5 h-3.5"
                  />
                </span>
                <span className="font-medium text-sm truncate pr-2">
                  {issue.title || issue.id}
                  {issue.message && (
                    <span className="ml-1.5 text-muted-foreground font-normal">
                      — {issue.message.length > 200 ? issue.message.slice(0, 200) + '…' : issue.message}
                    </span>
                  )}
                  {expandedId === issue.id && (
                    <span className="ml-1.5 text-blue-400 text-xs">▾</span>
                  )}
                </span>
                <span className="text-xs text-muted-foreground">{issue.service}</span>
                <span>
                  {issue.running_op ? (
                    <span className="inline-flex items-center gap-1 text-blue-400 text-xs font-medium">
                      <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                      {issue.running_op === 'investigate' ? 'Investigating…' : 'Fixing…'}
                    </span>
                  ) : (
                    <StageIndicator stage={issue.stage} />
                  )}
                </span>
                <span>
                  <InvestigationBadge type="confidence" value={issue.confidence} />
                </span>
                <span>
                  <InvestigationBadge type="complexity" value={issue.complexity} />
                </span>
                <span>
                  {issue.code_fixable === 'Yes' && <span className="text-green-400 text-sm">✓</span>}
                  {issue.code_fixable === 'Partially' && <span className="text-yellow-400 text-sm">~</span>}
                  {issue.code_fixable === 'No' && <span className="text-red-400 text-sm">✗</span>}
                  {!issue.code_fixable && <span className="text-muted-foreground text-xs">—</span>}
                </span>
                <span>
                  <CIStatusBadge status={issue.ci_status} url={issue.ci_url || undefined} />
                </span>
                <span>
                  {issue.mr_url ? (
                    <a
                      href={issue.mr_url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-blue-400 text-xs hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      MR ↗
                    </a>
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </span>
              </div>

              {/* Expanded row */}
              {expandedId === issue.id && (
                <div className="border-l-2 border-blue-500 bg-blue-950/20 px-4 py-3 space-y-3">
                  {/* Metadata row */}
                  <div className="flex flex-wrap gap-4 items-center text-xs">
                    {issue.service && (
                      <span className="text-muted-foreground">
                        Service <strong className="text-foreground">{issue.service}</strong>
                      </span>
                    )}
                    {issue.env && (
                      <span className="text-muted-foreground">
                        Env <strong className="text-foreground">{issue.env}</strong>
                      </span>
                    )}
                    {issue.last_seen && (
                      <span className="text-muted-foreground">
                        Last seen <strong className="text-foreground">{new Date(issue.last_seen).toLocaleString()}</strong>
                      </span>
                    )}
                    {issue.count > 0 && (
                      <span className="text-muted-foreground">
                        Occurrences <strong className="text-foreground">{issue.count}</strong>
                      </span>
                    )}
                    {issue.datadog_url && (
                      <a
                        href={issue.datadog_url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-blue-400 hover:underline"
                        onClick={(e) => e.stopPropagation()}
                      >
                        Datadog ↗
                      </a>
                    )}
                    <Link
                      to={`/issues/${issue.id}`}
                      className="text-blue-400 hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      Full detail ↗
                    </Link>
                  </div>

                  {/* Stack trace */}
                  {issue.stack_trace && (
                    <StackTracePreview trace={issue.stack_trace} />
                  )}

                  {/* Actions */}
                  <div className="flex gap-2">
                    {issue.stage === 'scanned' && !issue.running_op && (
                      <Button
                        size="sm"
                        className="h-6 text-xs"
                        disabled={!!actionLoading[issue.id]}
                        onClick={(e) => {
                          e.stopPropagation();
                          handleInvestigate(issue.id);
                        }}
                      >
                        {actionLoading[issue.id] === 'investigate' ? 'Starting...' : 'Investigate'}
                      </Button>
                    )}
                    {issue.running_op && (
                      <span className="inline-flex items-center gap-1 text-blue-400 text-xs">
                        <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                        {issue.running_op === 'investigate' ? 'Investigating...' : 'Fixing...'}
                      </span>
                    )}
                    <Button
                      size="sm"
                      variant="outline"
                      className="h-6 text-xs"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleIgnore(issue.id, issue.ignored);
                      }}
                    >
                      {issue.ignored ? 'Unignore' : 'Ignore'}
                    </Button>
                  </div>
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function StackTracePreview({ trace }: { trace: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = trace.split('\n');
  const truncated = lines.length > 15;
  const displayLines = expanded ? lines : lines.slice(0, 15);

  return (
    <div className="text-xs">
      <pre className="p-3 bg-muted/30 rounded border border-border font-mono text-muted-foreground whitespace-pre-wrap overflow-auto max-h-80">
        {displayLines.join('\n')}
      </pre>
      {truncated && (
        <button
          className="mt-1 text-blue-400 hover:underline text-xs"
          onClick={(e) => {
            e.stopPropagation();
            setExpanded(!expanded);
          }}
        >
          {expanded ? 'Show less' : `Show more (${lines.length - 15} more lines)`}
        </button>
      )}
    </div>
  );
}
