const API_BASE = import.meta.env.VITE_API_URL || '';

export interface IssueListItem {
  id: string;
  stage: string;
  title: string;
  message: string;
  service: string;
  env: string;
  last_seen: string;
  count: number;
  mr_url: string | null;
  ignored: boolean;
  ci_status: string;
  ci_url: string;
  confidence: string;
  complexity: string;
  code_fixable: string;
  running_op?: 'investigate' | 'fix';
  datadog_url: string;
  stack_trace: string;
  datadog_status: string;
  regression_count: number;
}

export interface ResolveData {
  branch: string;
  mr_url: string;
  mr_status: string;
  service: string;
  datadog_issue_id: string;
  datadog_url: string;
  created_at: string;
}

export interface IssueDetail {
  id: string;
  stage: string;
  error: string;
  investigation: string | null;
  fix: string | null;
  resolve: ResolveData | null;
  ci_status: string;
  ci_url: string;
  running_op?: 'investigate' | 'fix';
  datadog_status: string;
  regression_count: number;
}

export async function listIssues(status?: string, showIgnored?: boolean): Promise<IssueListItem[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
  if (showIgnored) params.set('show_ignored', 'true');
  const res = await fetch(`${API_BASE}/api/issues?${params}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export async function getIssue(id: string): Promise<IssueDetail> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export async function triggerInvestigate(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/investigate`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function triggerFix(id: string, iterate = false): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/fix`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ iterate }),
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function triggerScan(): Promise<void> {
  const res = await fetch(`${API_BASE}/api/scan`, { method: 'POST' });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function importIssue(issueId: string): Promise<{ status: string; id: string }> {
  const res = await fetch(`${API_BASE}/api/import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ issue_id: issueId }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
    throw new Error(err.error || `API error: ${res.status}`);
  }
  return res.json();
}

export async function ignoreIssue(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/ignore`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function unignoreIssue(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/unignore`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export function subscribeProgress(
  id: string,
  onMessage: (data: { status: string; message?: string; log?: string }) => void
): EventSource {
  const es = new EventSource(`${API_BASE}/api/issues/${encodeURIComponent(id)}/progress`);
  es.onmessage = (event) => {
    try {
      onMessage(JSON.parse(event.data));
    } catch {
      // ignore malformed events
    }
  };
  return es;
}

export async function fetchMRStatus(id: string): Promise<{ ci_status: string; ci_url: string; mr_status: string }> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/mr-status`);
  if (!res.ok) throw new Error(`API error: ${res.status}`);
  return res.json();
}

export interface SSEEvent {
  type: 'scan:complete' | 'issue:updated' | 'issue:progress' | 'issue:imported' | 'issue:resolved' | 'issue:regression' | 'issue:status_changed';
  payload: Record<string, any>;
}

export function subscribeEvents(onEvent: (event: SSEEvent) => void): EventSource {
  const es = new EventSource(`${API_BASE}/api/events`);
  es.onmessage = (msg) => {
    try {
      onEvent(JSON.parse(msg.data));
    } catch {
      // ignore malformed
    }
  };
  return es;
}
