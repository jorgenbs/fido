/**
 * Headless browser verification — run from web/ directory.
 * Usage: node verify.mjs
 * Exits 0 if no React/component errors, 1 if errors found.
 * Requires: npm install playwright (already in devDependencies)
 * Requires: Vite dev server running on http://localhost:5174
 */
import { chromium } from 'playwright';

const BASE = 'http://localhost:5174';

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();

const isBackendErr = (e) =>
  e.includes('Failed to fetch') ||
  e.includes('Failed to load resource') ||
  e.includes('net::ERR') ||
  e.includes('API error');

async function checkPage(url, label) {
  const errors = [];
  const onErr = (msg) => { if (msg.type() === 'error') errors.push(msg.text()); };
  const onUncaught = (err) => errors.push(`[uncaught] ${err.message}`);
  page.on('console', onErr);
  page.on('pageerror', onUncaught);

  await page.goto(url, { waitUntil: 'domcontentloaded', timeout: 10000 });
  await page.waitForTimeout(1000);

  page.off('console', onErr);
  page.off('pageerror', onUncaught);

  const real = errors.filter((e) => !isBackendErr(e));
  console.log(`\n--- ${label} ---`);
  real.length ? real.forEach((e) => console.log('  ERROR:', e)) : console.log('  OK');
  return real.length;
}

let totalErrors = 0;
totalErrors += await checkPage(BASE, 'Dashboard');
totalErrors += await checkPage(`${BASE}/issues/test-id`, 'IssueDetail');

await browser.close();

if (totalErrors > 0) {
  console.log(`\n✘ ${totalErrors} error(s) found`);
  process.exit(1);
} else {
  console.log('\n✓ No errors');
}
