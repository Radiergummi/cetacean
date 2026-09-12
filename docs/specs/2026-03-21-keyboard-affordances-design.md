# Keyboard Affordances Design

**Date:** 2026-03-21
**Status:** Approved

## Goal

Add comprehensive keyboard affordances for power users: focus management, section navigation, resource prev/next, keyboard hints, table sort reachability, view mode shortcut, and Cmd/Ctrl+F override.

## Shared Primitives

### KeyboardHintContext (~50 lines)

React context wrapping `Layout` that exposes `{ hintsVisible: boolean }`.

- `keydown` on Alt/Option starts a 1s timeout
- If the timeout fires without `keyup`, sets `hintsVisible = true`
- `keyup` on Alt/Option clears timeout, sets `hintsVisible = false`
- `blur` on window also clears (handles Alt+Tab away from browser)

`ShortcutTooltip` reads this context and shows immediately (no hover delay) when `hintsVisible` is true.

### useSectionNavigation (~30 lines)

Hook for detail pages. Queries all `[aria-expanded]` buttons within `<main>`, maintains a focus index.

- `}` (Shift+`]`) focuses the next section header
- `{` (Shift+`[`) focuses the previous section header
- Wraps around at ends
- Enter/Space on a focused section header toggles it (native button behavior)
- Registered via `useHotkeys`; `isEditing()` guard prevents interference with typing braces in editors

### useResourceNavigation

Hook for detail pages. Accepts resource type and current ID/name.

- On mount, fetches the list endpoint with the current URL's `sort`/`dir` params (falls back to API default sort if no params present, e.g., direct navigation to a detail page)
- Finds current resource position in the returned list
- `[` navigates to previous, `]` to next, wrapping around
- List is fetched once on mount and cached in a ref (stable order during visit)
- Renders a subtle prev/next hint in `PageHeader` breadcrumb area

Used by: NodeDetail, ServiceDetail, TaskDetail, ConfigDetail, SecretDetail, NetworkDetail, VolumeDetail, StackDetail.

### useSearchFocus

Hook for pages with in-page search. Accepts a ref to the search input element.

- Listens for `keydown` with `metaKey`/`ctrlKey` + `f`
- First press: if input is not focused, `preventDefault()` and focus it
- Second press: if input is already focused, do nothing (browser native Ctrl+F opens)
- Pages without this hook let Cmd+F pass through immediately

Used by: list pages (focuses `SearchInput` in `ListToolbar`) and `LogViewer` (focuses `LogSearch` input).

## Feature: Focus Trapping & Restoration

Leverage existing Base UI (`@base-ui/react@1.3.0`) primitives where possible. `initialFocus` and `finalFocus` are props on `Dialog.Popup` (i.e., `DialogContent` in the project's wrapper), not on `Dialog.Root`.

### SearchPalette

Refactor from manual `createPortal` to project's `Dialog` component with `modal={true}`. Provides focus trapping + restoration automatically. `initialFocus` on `DialogContent` pointed at the search input ref.

### ShortcutsHelp

Refactor from `createPortal` to `Dialog` with `modal={true}`.

### AlertDialog

Already uses Base UI's AlertDialog primitive with `modal={true}`. Verify focus trapping is working (should be by default).

### Inline Editors (EnvEditor, LabelsEditor, PortsEditor, etc.)

Base UI's Dialog renders content in a portal, which conflicts with inline editors that visually replace content in-place. Instead, use a lightweight `FocusTrap` component (~40 lines): two visually-hidden sentinel `<div tabIndex={0}>` elements at the start and end of the editor. When focus hits a sentinel via Tab, it wraps to the other end. Sentinels are `aria-hidden`. A `useFocusRestore` hook (~15 lines) captures `document.activeElement` on mount (the pencil button) and restores focus to it on unmount (save/cancel).

## Feature: Section Navigation

`{`/`}` for fast section jumping on detail pages. Section headers (`SectionToggle` buttons) are already natively tabbable, so Tab also works for linear navigation. The `{`/`}` keys skip over section contents for speed.

## Feature: Prev/Next Resource (`[`/`]`)

Detail pages navigate to the previous/next resource in the list, following the list page's current sort order. Wraps around at ends. Visual hint shown in PageHeader.

## Feature: View Mode Shortcut (`v`)

Registered via `useHotkeys` on list pages. Calls the same toggle function as `ViewToggle`. `ViewToggle` button gets a `ShortcutTooltip` with `keys={["v"]}`.

## Feature: Table Sort via Keyboard

`DataTable` column headers with `onHeaderClick` get `tabIndex={0}`, `role="button"`, `onKeyDown` (Enter/Space triggers `onHeaderClick`), and `focus-visible:ring` styling. No new shortcuts needed.

## Feature: Cmd/Ctrl+F Override

First press focuses in-page search (list toolbar filter or log search). Second press falls through to browser native. Only active on pages that register `useSearchFocus`.

## Feature: Alt/Option Hold Hints

Holding Alt/Option for >= 1s reveals all `ShortcutTooltip`s on the page simultaneously. Releasing hides them. Uses `KeyboardHintContext`.

New tooltips added to:
- View toggle (`v`)
- Keyboard shortcuts button (`?`)
- First/last section headers (`{`/`}`)
- Prev/next indicator in PageHeader (`[`/`]`)

Not added to context-dependent shortcuts (j/k in tables, Enter on rows) to avoid noise.

## Feature: Tab-Cycling Through Action Buttons

Mostly verification of existing markup:

1. Verify tab order on detail pages flows logically through PageHeader actions
2. Verify editor pencil buttons have `aria-label` for screen reader context
3. Confirmation dialogs: set `initialFocus` to Cancel button ref (safer default for destructive actions)

## ShortcutsHelp Updates

Add new entries to the `groups` array:
- **Lists:** `v` (toggle view)
- **Detail pages:** `[`/`]` (prev/next resource), `{`/`}` (prev/next section)
- **Global:** `Cmd+F` (focus search), `Alt hold` (show all shortcuts)

## Files Changed

### New files
- `frontend/src/hooks/useSectionNavigation.ts`
- `frontend/src/hooks/useResourceNavigation.ts`
- `frontend/src/hooks/useSearchFocus.ts`
- `frontend/src/hooks/useKeyboardHints.ts` (context + provider)
- `frontend/src/components/FocusTrap.tsx` (lightweight sentinel-based trap for inline editors)
- `frontend/src/hooks/useFocusRestore.ts` (captures and restores activeElement)

### Modified files
- `frontend/src/App.tsx` — wrap Layout with `KeyboardHintProvider`
- `frontend/src/components/ShortcutTooltip.tsx` — read `KeyboardHintContext`, show when `hintsVisible`
- `frontend/src/components/ShortcutsHelp.tsx` — refactor to Dialog, add new shortcut entries
- `frontend/src/components/search/SearchPalette.tsx` — refactor to Dialog
- `frontend/src/components/DataTable.tsx` — add tabIndex/role/onKeyDown to sortable column headers
- `frontend/src/components/CollapsibleSection.tsx` — no changes needed (already a button)
- `frontend/src/components/ViewToggle.tsx` — add ShortcutTooltip
- `frontend/src/components/ListToolbar.tsx` — pass search input ref for useSearchFocus
- `frontend/src/pages/*Detail.tsx` (8 files) — add useSectionNavigation, useResourceNavigation
- `frontend/src/pages/*List.tsx` (8 files) — add useSearchFocus, register `v` hotkey
- `frontend/src/components/log/LogViewer.tsx` or `LogSearch.tsx` — add useSearchFocus
- `frontend/src/components/service-detail/*.tsx` — wrap editors with FocusTrap + useFocusRestore
- `frontend/src/components/ui/alert-dialog.tsx` — verify/configure initialFocus for Cancel button
