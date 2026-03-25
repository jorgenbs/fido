const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:8080';

export interface IssueListItem {
  id: string;
  stage: string;
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
}

export async function listIssues(status?: string): Promise<IssueListItem[]> {
  const params = new URLSearchParams();
  if (status) params.set('status', status);
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

export async function triggerFix(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/api/issues/${encodeURIComponent(id)}/fix`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export async function triggerScan(): Promise<void> {
  const res = await fetch(`${API_BASE}/api/scan`, { method: 'POST' });
  if (!res.ok) throw new Error(`API error: ${res.status}`);
}

export function subscribeProgress(id: string, onMessage: (data: string) => void): EventSource {
  const es = new EventSource(`${API_BASE}/api/issues/${encodeURIComponent(id)}/progress`);
  es.onmessage = (event) => onMessage(event.data);
  return es;
}
