/**
 * Notification & SSE debug verification.
 * Usage: node verify-notifications.mjs
 *
 * Requires:
 *   - fido serve running (default :8080, or set FIDO_PORT)
 *   - Playwright installed
 *
 * Tests:
 *   1. SSE /api/events connects and receives events
 *   2. Notification permission can be granted
 *   3. notify() fires when a test event arrives
 */
import { chromium } from 'playwright';

const PORT = process.env.FIDO_PORT || '8080';
const BASE = `http://localhost:${PORT}`;

console.log(`Testing against ${BASE}\n`);

// --- Step 1: Verify SSE endpoint delivers events ---
console.log('1. Testing SSE event delivery...');
const sseOk = await new Promise(async (resolve) => {
  const timer = setTimeout(() => { console.log('   FAIL: SSE timed out'); resolve(false); }, 5000);

  // Subscribe to SSE in background
  const ctrl = new AbortController();
  fetch(`${BASE}/api/events`, { signal: ctrl.signal })
    .then(async (res) => {
      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        const text = decoder.decode(value);
        if (text.includes('"type"')) {
          console.log('   OK: received SSE event:', text.trim());
          clearTimeout(timer);
          ctrl.abort();
          resolve(true);
          break;
        }
      }
    })
    .catch(() => {}); // abort throws

  // Fire a test event
  await new Promise(r => setTimeout(r, 500));
  await fetch(`${BASE}/api/debug/event`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ type: 'issue:updated', payload: { id: 'test-debug', field: 'stage', newValue: 'investigated' } }),
  });
});

if (!sseOk) {
  console.log('\nSSE delivery failed — notifications cannot work without it.');
  process.exit(1);
}

// --- Step 2: Browser notification test ---
console.log('\n2. Testing browser notification flow...');

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext();
const page = await context.newPage();

// Stub Notification API so headless Chrome reports 'granted'
await page.addInitScript(() => {
  window.__notifications = [];
  class MockNotification {
    static permission = 'granted';
    static requestPermission() { return Promise.resolve('granted'); }
    constructor(title, options) {
      console.log('[notify] fired:', title);
      window.__notifications.push({ title, ...options });
      this.onclick = null;
    }
    close() {}
  }
  window.Notification = MockNotification;
});

// Collect console logs
const logs = [];
page.on('console', (msg) => {
  const text = msg.text();
  if (text.startsWith('[notify]') || text.startsWith('[sse]')) {
    logs.push(text);
  }
});

// Navigate to dashboard
await page.goto(BASE, { waitUntil: 'load', timeout: 10000 });
await page.waitForTimeout(1000);

console.log('   Page loaded, checking permission state...');

// Check notification permission in browser
const permState = await page.evaluate(() => {
  return typeof Notification !== 'undefined' ? Notification.permission : 'unavailable';
});
console.log(`   Notification.permission = "${permState}"`);

// Fire a test event to trigger notification
console.log('   Sending test event...');
await fetch(`${BASE}/api/debug/event`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ type: 'issue:updated', payload: { id: 'test-debug', field: 'stage', newValue: 'investigated' } }),
});

await page.waitForTimeout(2000);

console.log('\n3. Console trace:');
if (logs.length === 0) {
  console.log('   FAIL: No [notify] or [sse] logs captured');
  console.log('   This means either:');
  console.log('   - SSE EventSource never connected');
  console.log('   - Event never reached the switch case');
  console.log('   - useEventStream hook never mounted');
} else {
  logs.forEach(l => console.log(`   ${l}`));

  const sseReceived = logs.some(l => l.startsWith('[sse]'));
  const notifyCalled = logs.some(l => l.startsWith('[notify]'));
  const notifyFired = logs.some(l => l.includes('fired:'));
  const notifySkipped = logs.some(l => l.includes('skipped:'));

  console.log('\n4. Diagnosis:');
  console.log(`   SSE event received:   ${sseReceived ? 'YES' : 'NO'}`);
  console.log(`   notify() called:      ${notifyCalled ? 'YES' : 'NO'}`);
  console.log(`   Notification fired:   ${notifyFired ? 'YES' : 'NO'}`);
  if (notifySkipped) {
    const skipLog = logs.find(l => l.includes('skipped:'));
    console.log(`   Skipped reason:       ${skipLog}`);
  }
}

await browser.close();

const success = logs.some(l => l.includes('fired:'));
console.log(success ? '\n OK: Notification flow works' : '\n FAIL: Notification did not fire');
process.exit(success ? 0 : 1);
