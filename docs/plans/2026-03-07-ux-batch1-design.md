# UX Improvements Batch 1

## 1. Relative Timestamps

Add a `timeAgo(date: string): string` utility that returns human-readable relative times: "just now", "2m ago", "3h ago", "2d ago", etc. Thresholds: <60s → "just now", <60m → "Xm ago", <24h → "Xh ago", <30d → "Xd ago", else formatted date.

Render as `<time datetime={iso} title={fullFormatted}>{relative}</time>` so hovering shows the exact timestamp.

Apply to: task created/updated timestamps in ServiceDetail and TaskDetail, node/service updated times in list pages, config/secret/volume timestamps in their list pages.

## 2. Sticky Table Headers

Add `sticky top-0 z-10 bg-background` to `<TableHeader>` in `frontend/src/components/ui/table.tsx`. Applies globally to all list pages.

## 3. Detail Page Enrichment

### ServiceDetail
- Labels section: render `service.Spec.Labels` as key-value badges. If >5, show first 5 with a "Show all" toggle.
- Placement constraints already shown via KVTable — no change needed.

### NodeDetail
- Resource info: show CPU count from `node.Description.Resources.NanoCPUs` and memory from `node.Description.Resources.MemoryBytes` in the info cards.
- Manager status: if `node.ManagerStatus` exists, show leader boolean and reachability.

### StackDetail
- Resource summary in PageHeader subtitle: "3 services, 1 config, 2 secrets"
- Per-service row: show running/total task count (e.g., "2/3 running"). Requires fetching tasks for the stack's services — data already available from `/api/tasks?service=X` or from SSE cache.
