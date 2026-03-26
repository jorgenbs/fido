const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export interface IssueListItem {
  id: string;
  stage: string;
  title: string;
  service: string;
  last_seen: string;
  count: number;
  mr_url: string | null;
  ignored: boolean;
  ci_status: string;
  ci_url: string;
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
