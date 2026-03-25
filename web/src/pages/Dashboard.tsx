import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { listIssues, triggerScan, type IssueListItem } from '../api/client';
import { StageIndicator } from '../components/StageIndicator';

export function Dashboard() {
  const [issues, setIssues] = useState<IssueListItem[]>([]);
  const [filter, setFilter] = useState('');
  const [loading, setLoading] = useState(true);

  const fetchIssues = async () => {
    setLoading(true);
    try {
      const data = await listIssues(filter || undefined);
      setIssues(data);
    } catch (err) {
      console.error('Failed to fetch issues:', err);
    }
    setLoading(false);
  };

  useEffect(() => {
    fetchIssues();
  }, [filter]);

  const handleScan = async () => {
    await triggerScan();
    fetchIssues();
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem' }}>
        <h2>Issues</h2>
        <div>
          <select value={filter} onChange={(e) => setFilter(e.target.value)} style={{ marginRight: '0.5rem' }}>
            <option value="">All stages</option>
            <option value="scanned">Scanned</option>
            <option value="investigated">Investigated</option>
            <option value="fixed">Fixed</option>
          </select>
          <button onClick={handleScan}>Scan Now</button>
        </div>
      </div>

      {loading ? (
        <p>Loading...</p>
      ) : issues.length === 0 ? (
        <p>No issues found. Run a scan to get started.</p>
      ) : (
        <table style={{ width: '100%', borderCollapse: 'collapse' }}>
          <thead>
            <tr style={{ borderBottom: '2px solid #e5e7eb' }}>
              <th style={{ textAlign: 'left', padding: '0.5rem' }}>Issue ID</th>
              <th style={{ textAlign: 'left', padding: '0.5rem' }}>Stage</th>
            </tr>
          </thead>
          <tbody>
            {issues.map((issue) => (
              <tr key={issue.id} style={{ borderBottom: '1px solid #e5e7eb' }}>
                <td style={{ padding: '0.5rem' }}>
                  <Link to={`/issues/${issue.id}`}>{issue.id}</Link>
                </td>
                <td style={{ padding: '0.5rem' }}>
                  <StageIndicator stage={issue.stage} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}
