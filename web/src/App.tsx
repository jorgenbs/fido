import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { IssueDetail } from './pages/IssueDetail';

export function App() {
  return (
    <BrowserRouter>
      <div style={{ maxWidth: '1200px', margin: '0 auto', padding: '1rem' }}>
        <h1>Fido</h1>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/issues/:id" element={<IssueDetail />} />
        </Routes>
      </div>
    </BrowserRouter>
  );
}
