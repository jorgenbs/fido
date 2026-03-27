# Checkbox Range Selection — Design Spec

**Date:** 2026-03-27
**File:** `web/src/pages/Dashboard.tsx`

## Summary

Add shift+click range selection to the issue list checkboxes. Clicking a checkbox selects/deselects it (plain toggle). Shift+clicking a second checkbox selects all rows between the two clicks, matching standard list behaviour (Gmail, Finder).

## State

Add a single ref alongside existing selection state:

```ts
const lastSelectedRef = useRef<string | null>(null);
```

A ref (not state) is correct here — the anchor never needs to trigger a re-render.

## Updated `toggleSelectOne`

Signature change: `toggleSelectOne(id: string, e: React.MouseEvent)`

**Plain click (no shift, or no anchor):**
1. Toggle `id` in `selectedIds` as before.
2. Set `lastSelectedRef.current = id`.

**Shift+click (shift held and anchor exists):**
1. Find `anchorIdx` and `clickIdx` in `filteredIssues`.
2. Compute `[minIdx, maxIdx]` from the two indices.
3. Add all IDs in `filteredIssues[minIdx..maxIdx]` to `selectedIds` (union, never deselect).
4. Leave `lastSelectedRef.current` unchanged (anchor stays fixed for repeated extends).

**Shift+click with no anchor:** fall through to plain-click behaviour.

## Call Site

`onCheckedChange` on the shadcn `<Checkbox>` does not carry a `MouseEvent`. Switch the wrapping `<span>` (which already calls `e.stopPropagation()`) to an `onClick` handler that calls `toggleSelectOne(issue.id, e)`. Remove the separate `onCheckedChange` prop; the checked state is still driven by `selectedIds`.

## Anchor Reset

- **Select-all / clear-all (`toggleSelectAll`):** reset `lastSelectedRef.current = null`.
- **Filter/data change (`fetchIssues`):** leave anchor as-is; the next plain click will establish a new anchor naturally.

## Edge Cases

| Scenario | Behaviour |
|---|---|
| Shift-click with nothing previously selected | Treated as plain click (no anchor yet) |
| Repeated shift-clicks from same anchor | Range extends from anchor each time |
| Shift-click after select-all | Anchor is null → plain click behaviour |
| Clicked item is the anchor itself | Range is length 1 → same as plain toggle |

## Out of Scope

- Keyboard navigation (arrow + space/enter selection)
- Drag-to-select
- Deselecting a range via shift+click
