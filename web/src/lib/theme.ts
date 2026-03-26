const DARK_KEY = 'fido-theme';

export function getInitialTheme(): 'dark' | 'light' {
  const stored = localStorage.getItem(DARK_KEY);
  if (stored === 'light') return 'light';
  return 'dark'; // default dark
}

export function applyTheme(theme: 'dark' | 'light') {
  const root = document.documentElement;
  if (theme === 'dark') {
    root.classList.add('dark');
  } else {
    root.classList.remove('dark');
  }
  localStorage.setItem(DARK_KEY, theme);
}

export function toggleTheme(): 'dark' | 'light' {
  const current = document.documentElement.classList.contains('dark') ? 'dark' : 'light';
  const next = current === 'dark' ? 'light' : 'dark';
  applyTheme(next);
  return next;
}
