import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  listIssues,
  triggerScan,
  ignoreIssue,
  unignoreIssue,
  type IssueListItem,
} from '../api/client';
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
  const lastSelectedRef = useRef<string | null>(null);
  const [serviceFilter, setServiceFilter] = useState('all');
  const [confidenceFilter, setConfidenceFilter] = useState('all');
  const [complexityFilter, setComplexityFilter] = useState('all');
  const [codeFixableOnly, setCodeFixableOnly] = useState(false);

  const fetchIssues = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listIssues(filter === 'all' ? undefined : filter, showIgnored);
      setIssues(data);
      setSelectedIds(new Set());
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    } finally {
      setLoading(false);
    }
  }, [filter, showIgnored]);

  useEffect(() => {
    fetchIssues();
  }, [fetchIssues]);

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

      {someSelected && (
        <div className="flex items-center gap-3 px-4 py-2 bg-blue-950/30 border-b border-blue-900 text-xs">
          <span className="text-blue-300 font-medium">{selectedIds.size} selected</span>
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
        </div>
      )}

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
          <div className="grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-2 bg-muted/50 text-xs font-semibold text-muted-foreground tracking-wide uppercase border-b border-border">
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
                className={`grid grid-cols-[32px_2.5fr_1fr_0.8fr_0.8fr_0.6fr_0.5fr_0.8fr_0.8fr] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors ${selectedIds.has(issue.id) ? 'bg-blue-950/30' : ''}`}
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
                <div className="border-l-2 border-blue-500 bg-blue-950/20 px-4 py-3">
                  <div className="flex flex-wrap gap-4 items-center text-xs">
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
                    <Link
                      to={`/issues/${issue.id}`}
                      className="text-blue-400 hover:underline"
                      onClick={(e) => e.stopPropagation()}
                    >
                      View full detail ↗
                    </Link>
                    <div className="flex gap-2 ml-auto">
                      {issue.stage === 'scanned' && (
                        <Link to={`/issues/${issue.id}`}>
                          <Button size="sm" className="h-6 text-xs" onClick={(e) => e.stopPropagation()}>
                            Investigate
                          </Button>
                        </Link>
                      )}
                      {issue.stage === 'investigated' && (
                        <Link to={`/issues/${issue.id}`}>
                          <Button size="sm" className="h-6 text-xs" onClick={(e) => e.stopPropagation()}>
                            Fix
                          </Button>
                        </Link>
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
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
