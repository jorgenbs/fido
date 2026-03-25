import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { getIssue, triggerInvestigate, triggerFix, type IssueDetail as IssueDetailType } from '../api/client';
import { StageIndicator } from '../components/StageIndicator';
import { MarkdownViewer } from '../components/MarkdownViewer';

export function IssueDetail() {
  const { id } = useParams<{ id: string }>();
  const [issue, setIssue] = useState<IssueDetailType | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchIssue = async () => {
    if (!id) return;
    setLoading(true);
    try {
      const data = await getIssue(id);
      setIssue(data);
    } catch (err) {
      console.error('Failed to fetch issue:', err);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchIssue();
  }, [id]);

  if (loading) return <p>Loading...</p>;
  if (!issue) return <p>Issue not found</p>;

  const handleInvestigate = async () => {
    await triggerInvestigate(issue.id);
    fetchIssue();
  };

  const handleFix = async () => {
    await triggerFix(issue.id);
    fetchIssue();
  };

  return (
    <div>
      <Link to="/">&larr; Back to dashboard</Link>
      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem', margin: '1rem 0' }}>
        <h2 style={{ margin: 0 }}>{issue.id}</h2>
        <StageIndicator stage={issue.stage} />
      </div>

      <MarkdownViewer title="Error Report" content={issue.error} />

      {issue.investigation ? (
        <MarkdownViewer title="Investigation" content={issue.investigation} />
      ) : (
        <button onClick={handleInvestigate}>Investigate this issue</button>
      )}

      {issue.fix ? (
        <MarkdownViewer title="Fix" content={issue.fix} />
      ) : issue.investigation ? (
        <button onClick={handleFix}>Fix this issue</button>
      ) : null}

      {issue.resolve && (
        <div style={{ border: '1px solid #10b981', borderRadius: '8px', padding: '1rem', marginTop: '1rem' }}>
          <h3 style={{ marginTop: 0 }}>Resolution</h3>
          <p><strong>Branch:</strong> {issue.resolve.branch}</p>
          <p><strong>MR:</strong> <a href={issue.resolve.mr_url} target="_blank" rel="noreferrer">{issue.resolve.mr_url}</a></p>
          <p><strong>Status:</strong> {issue.resolve.mr_status}</p>
          <p><strong>Created:</strong> {issue.resolve.created_at}</p>
        </div>
      )}
    </div>
  );
}
