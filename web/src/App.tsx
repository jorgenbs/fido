import { useEffect } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { IssueDetail } from './pages/IssueDetail';
import { applyTheme, getInitialTheme } from './lib/theme';

export default function App() {
  useEffect(() => {
    applyTheme(getInitialTheme());
  }, []);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/issues/:id" element={<IssueDetail />} />
      </Routes>
    </BrowserRouter>
  );
}
