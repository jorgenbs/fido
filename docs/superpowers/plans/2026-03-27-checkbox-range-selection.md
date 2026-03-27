# Checkbox Range Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shift+click range selection to the issue list checkboxes in the Dashboard.

**Architecture:** Add a `useRef` anchor to track the last-clicked checkbox, update `toggleSelectOne` to detect `event.shiftKey` and select the slice between anchor and new click, and switch the call site from `onCheckedChange` to `onClick` so the `MouseEvent` is available.

**Tech Stack:** React (useRef, useState), TypeScript, Playwright (verify.mjs) for verification.

---

### Task 1: Add anchor ref and range logic to `toggleSelectOne`

**Files:**
- Modify: `web/src/pages/Dashboard.tsx`

- [ ] **Step 1: Add the anchor ref** after the existing `selectedIds` state declaration (line 24)

Open `web/src/pages/Dashboard.tsx`. After:
```ts
const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
```
Add:
```ts
const lastSelectedRef = useRef<string | null>(null);
```

Also add `useRef` to the React import at the top of the file:
```ts
import { useCallback, useEffect, useRef, useState } from 'react';
```

- [ ] **Step 2: Replace `toggleSelectOne` with the range-aware version**

Replace the existing function (lines 102–109):
```ts
const toggleSelectOne = (id: string) => {
  setSelectedIds(prev => {
    const next = new Set(prev);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
};
```

With:
```ts
const toggleSelectOne = (id: string, e: React.MouseEvent) => {
  const anchor = lastSelectedRef.current;
  if (e.shiftKey && anchor && anchor !== id) {
    const anchorIdx = filteredIssues.findIndex(i => i.id === anchor);
    const clickIdx = filteredIssues.findIndex(i => i.id === id);
    if (anchorIdx !== -1 && clickIdx !== -1) {
      const [min, max] = anchorIdx < clickIdx
        ? [anchorIdx, clickIdx]
        : [clickIdx, anchorIdx];
      setSelectedIds(prev => {
        const next = new Set(prev);
        filteredIssues.slice(min, max + 1).forEach(i => next.add(i.id));
        return next;
      });
      return;
    }
  }
  // Plain toggle
  lastSelectedRef.current = id;
  setSelectedIds(prev => {
    const next = new Set(prev);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    return next;
  });
};
```

- [ ] **Step 3: Reset anchor in `toggleSelectAll`**

Replace the existing `toggleSelectAll` (lines 94–100):
```ts
const toggleSelectAll = () => {
  if (allSelected) {
    setSelectedIds(new Set());
  } else {
    setSelectedIds(new Set(filteredIssues.map(i => i.id)));
  }
};
```

With:
```ts
const toggleSelectAll = () => {
  lastSelectedRef.current = null;
  if (allSelected) {
    setSelectedIds(new Set());
  } else {
    setSelectedIds(new Set(filteredIssues.map(i => i.id)));
  }
};
```

- [ ] **Step 4: Update the row call site to pass the MouseEvent**

Find the per-row checkbox (around line 257):
```tsx
<span onClick={(e) => e.stopPropagation()}>
  <Checkbox
    checked={selectedIds.has(issue.id)}
    onCheckedChange={() => toggleSelectOne(issue.id)}
    className="w-3.5 h-3.5"
  />
</span>
```

Replace with:
```tsx
<span
  onClick={(e) => {
    e.stopPropagation();
    toggleSelectOne(issue.id, e);
  }}
>
  <Checkbox
    checked={selectedIds.has(issue.id)}
    onCheckedChange={() => {}}
    className="w-3.5 h-3.5"
  />
</span>
```

`onCheckedChange` becomes a no-op — the span's `onClick` is the sole driver of selection logic, which gives us access to `MouseEvent` (needed for `e.shiftKey`). The no-op prevents React warnings about a controlled Radix Checkbox with no change handler.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/Dashboard.tsx
git commit -m "feat: shift+click range selection for issue checkboxes"
```

---

### Task 2: Verify with Playwright and fix any issues

**Files:**
- Read: `web/verify.mjs` (to understand what it checks)

- [ ] **Step 1: Start the dev server**

In a background terminal:
```bash
cd web && npm run dev
```
Wait until you see `Local: http://localhost:5174/`.

- [ ] **Step 2: Run Playwright verification**

```bash
cd web && node verify.mjs
```

Expected: exits 0, output like:
```
✓ Dashboard loaded
✓ IssueDetail loaded
No React errors found.
```

If it exits 1, read the error output and fix the reported React console errors before proceeding.

- [ ] **Step 3: Manual smoke-test in browser**

Open `http://localhost:5174/` in a browser.

1. Click a checkbox on row 1 — it should check.
2. Shift+click a checkbox on row 4 — rows 1–4 should all become checked.
3. Shift+click row 2 — rows 1–2 should be checked (anchor stays at row 1).
4. Click the header "select all" checkbox, then click a single row checkbox — anchor resets, only that row toggles.

- [ ] **Step 4: Kill dev server**

```bash
kill $(pgrep -f 'npm run dev') 2>/dev/null || true
```
