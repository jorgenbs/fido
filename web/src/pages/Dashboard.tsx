import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  listIssues,
  triggerScan,
  ignoreIssue,
  unignoreIssue,
  type IssueListItem,
} from '../api/client';
import { StageIndicator } from '../components/StageIndicator';
import { Button } from '../components/ui/button';
import { Checkbox } from '../components/ui/checkbox';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../components/ui/select';
import { toggleTheme } from '../lib/theme';

export function Dashboard() {
  const [issues, setIssues] = useState<IssueListItem[]>([]);
  const [filter, setFilter] = useState('');
  const [showIgnored, setShowIgnored] = useState(false);
  const [loading, setLoading] = useState(true);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const fetchIssues = async () => {
    setLoading(true);
    try {
      const data = await listIssues(filter || undefined, showIgnored);
      setIssues(data);
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchIssues();
  }, [filter, showIgnored]);

  const handleScan = async () => {
    await triggerScan();
    fetchIssues();
  };

  const handleIgnore = async (id: string, currentlyIgnored: boolean) => {
    try {
      if (currentlyIgnored) {
        await unignoreIssue(id);
      } else {
        await ignoreIssue(id);
      }
      fetchIssues();
    } catch (err) {
      console.error('Failed to toggle ignore:', err);
    }
  };

  const toggleRow = (id: string) => {
    setExpandedId(expandedId === id ? null : id);
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
            {issues.length}
          </span>
        </div>
        <div className="flex items-center gap-3">
          <Select value={filter} onValueChange={setFilter}>
            <SelectTrigger className="w-36 h-7 text-xs">
              <SelectValue placeholder="All stages" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="">All stages</SelectItem>
              <SelectItem value="scanned">Scanned</SelectItem>
              <SelectItem value="investigated">Investigated</SelectItem>
              <SelectItem value="fixed">Fixed</SelectItem>
            </SelectContent>
          </Select>
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

      {/* Table */}
      {loading ? (
        <p className="p-4 text-sm text-muted-foreground">Loading…</p>
      ) : issues.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No issues found. Run a scan to get started.</p>
      ) : (
        <div>
          {/* Header row */}
          <div className="grid grid-cols-[2.5fr_1fr_1fr_0.8fr_60px] px-4 py-2 bg-muted/50 text-xs font-semibold text-muted-foreground tracking-wide uppercase border-b border-border">
            <span>Issue</span>
            <span>Service</span>
            <span>Stage</span>
            <span>MR</span>
            <span />
          </div>

          {issues.map((issue) => (
            <div key={issue.id} className="border-b border-border">
              {/* Main row */}
              <div
                className="grid grid-cols-[2.5fr_1fr_1fr_0.8fr_60px] px-4 py-3 items-center cursor-pointer hover:bg-muted/20 transition-colors"
                onClick={() => toggleRow(issue.id)}
              >
                <span className="font-medium text-sm truncate pr-2">
                  {issue.title || issue.id}
                  {expandedId === issue.id && (
                    <span className="ml-1.5 text-blue-400 text-xs">▾</span>
                  )}
                </span>
                <span className="text-xs text-muted-foreground">{issue.service}</span>
                <span>
                  <StageIndicator stage={issue.stage} />
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
                <span className="text-muted-foreground text-center text-sm">···</span>
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
