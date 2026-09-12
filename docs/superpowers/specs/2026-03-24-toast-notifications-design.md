# Toast Notifications for API Errors

## Summary

Add Sonner toast notifications to surface transient API errors without replacing functional UI. Toasts supplement existing inline error display — they do not replace it. Success toasts are out of scope.

## Decision Framework

Every error display site falls into one of three categories:

1. **Toast only** — The component can still render meaningfully (partial data, cached state, empty section). The user needs to know something failed, but the UI is not broken. Examples: mutation failures with no follow-up action, background refetch failures, transient 502s.

2. **Inline only** — The error requires the user to take a specific action *from that location* (force-remove button, retry with different input). The error must stay visible alongside the action affordance. Examples: `RemoveResourceAction` with `force-remove` action, `NodeActions` with `force-remove`.

3. **FetchError / ErrorBoundary** — The component cannot render without the data. The entire component is replaced with an error state. No change to this pattern.

## Infrastructure

Install Sonner via shadcn (`npx shadcn@latest add sonner`). Add `<Toaster />` to `App.tsx` inside the provider tree, after `ConnectionTracker` and before `Layout`. Pass `theme="system"` and `richColors` to match the existing theme toggle and get red error styling out of the box.

Use `toast.error()` directly from Sonner — no custom wrapper. Import `{ toast }` from `sonner` at call sites.

Position: bottom-right (Sonner default). Duration: 8 seconds for errors (default 4s is too short for reading suggestion text).

## Toast Content

When the error is an `ApiError` with a known code in the error dictionary:
- **Title**: `errorInfo.title`
- **Description**: `errorInfo.suggestion`

When the error is unrecognized:
- **Title**: the error message (from `ApiError.detail` or `Error.message`)
- No description

No "Details" link — the suggestion text already provides actionable guidance, and navigating away from the SPA to a server-rendered page is disruptive.

## Deduplication

For call sites that may fire repeatedly (SSE reconnects, periodic refetches), use Sonner's `id` option (`toast.error("msg", { id: "key" })`) to replace rather than stack. The `id` should be a stable string per error source (e.g., the SSE path or fetch URL).

## useAsyncAction Integration

Add a `toast` option to `useAsyncAction`. When `toast: true`:
- On error, fire `toast.error()` with the error info
- Do NOT set the inline `error`/`cause` state
- `loading` still works normally

When `toast: false` (default, current behavior):
- Inline `error`/`cause` state is set as today
- No toast

This keeps the hook backward-compatible. Callers opt in:

```ts
const scale = useAsyncAction({ toast: true });
const remove = useAsyncAction(); // inline errors, for force-remove flow
```

Today only `RemoveResourceAction` and `NodeActions` need inline errors (for force-remove). All other `useAsyncAction` callers can switch to `toast: true`. No component needs mixed mode — the two patterns are mutually exclusive per hook instance.

## Fetch Error Toasts

Per-call-site decision. Components that can render without fresh data call `toast.error()` in their catch block alongside whatever degraded state they show (stale data, empty section). No architectural change — just a `toast.error()` call at the catch site.

Components that cannot render without data continue using `FetchError` with no toast.

## What Does NOT Change

- `FetchError` component and its usage for page-level load failures
- `ErrorBoundary` for render crashes
- `RemoveResourceAction` / `NodeActions` inline error + force-remove flow
- `LogEmptyState` and `TimeSeriesChart` inline error states (they keep their current display; a toast may be added alongside where appropriate)
