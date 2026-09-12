# EditableTable Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract a generic `EditableTable<T>` component from `KeyValueEditor`, then refactor `KeyValueEditor` and all three attachment editors to use it.

**Architecture:** `EditableTable<T>` owns the edit lifecycle (editing, saving, draft, adding, escape cancel). Consumers provide render functions for cells and add-row, plus a save callback. `KeyValueEditor` becomes a thin wrapper that translates `Record<string, string>` ↔ `[string, string][]` at the boundary. Attachment editors become thin wrappers that manage their combobox state and resource fetching.

**Tech Stack:** React 19, TypeScript, existing UI components (CollapsibleSection, Button, Input, Spinner)

**Spec:** `docs/superpowers/specs/2026-03-23-editable-table-design.md`

---

### Task 1: Create EditableTable Component

**Files:**
- Create: `frontend/src/components/EditableTable.tsx`

- [ ] **Step 1: Create the EditableTable component**

Create `frontend/src/components/EditableTable.tsx`. This is a direct extraction of the edit-mode shell from `KeyValueEditor.tsx` — same class names, same structure, but generic over `T`.

The component must:
- Accept the props defined in the spec (see below)
- Manage internal state: `editing`, `saving`, `saveError`, `draft: T[]`, `adding`
- Render: CollapsibleSection → read-only view OR edit table with footer
- Handle: openEdit, cancelEdit, removeRow, "Add another" lifecycle, save with pending row commit

Key implementation details from the spec:
- `openEdit()`: `setDraft([...items])` (shallow copy of the array — individual items are not mutated in-place, so no deep clone needed). Sets `adding = items.length === 0`.
- "Add another" click: if `adding && canAdd`, call `onAddCommit()`. If null, no-op (keep add row). If `T`, append to draft, call `onAddReset()`, keep `adding = true`. If `adding && !canAdd`, button disabled.
- `save()`: if `adding && canAdd`, try `onAddCommit()` to build effective draft (null = skip). If `adding && !canAdd`, ignore pending row. Call `onSave(effectiveDraft)`.
- `renderAddKeyCell(draft)` and `renderAddValueCell(draft)` receive current draft
- `renderAddError?.()` renders a full-width colSpan=3 row beneath the add row when non-null
- Edit button uses `stopPropagation` on click (CollapsibleSection header is clickable)

Props interface:
```tsx
interface EditableTableProps<T> {
  title: string;
  titleExtra?: ReactNode;
  items: T[];
  columns: [string, string];
  defaultOpen?: boolean;
  editDisabled?: boolean;

  renderReadOnly: (items: T[]) => ReactNode;
  emptyLabel: string;
  emptyHint?: string;

  keyFn: (item: T, index: number) => string | number;
  renderKeyCell: (item: T, index: number) => ReactNode;
  renderValueCell: (item: T, index: number, update: (next: T) => void) => ReactNode;

  renderAddKeyCell: (draft: T[]) => ReactNode;
  renderAddValueCell: (draft: T[]) => ReactNode;
  renderAddError?: () => ReactNode;
  canAdd: boolean;
  onAddCommit: () => T | null;
  onAddReset: () => void;

  onSave: (items: T[]) => Promise<void>;
}
```

Use the exact class names from the current `KeyValueEditor` (lines 183-317): `overflow-x-auto rounded-lg border`, `w-full min-w-max whitespace-nowrap`, `bg-muted/50 dark:bg-transparent`, `border-b bg-transparent! last:border-b-0`, `flex items-center gap-2 p-3`, etc.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success (no consumers yet)

- [ ] **Step 3: Commit**

```bash
git add frontend/src/components/EditableTable.tsx
git commit -m "feat: extract generic EditableTable component from KeyValueEditor"
```

---

### Task 2: Refactor KeyValueEditor to Use EditableTable

**Files:**
- Modify: `frontend/src/components/KeyValueEditor.tsx`

- [ ] **Step 1: Read existing consumers to understand the API contract**

Read these files to confirm the API that must not change:
- `frontend/src/components/service-detail/EnvEditor.tsx` — uses `onSave: (ops: PatchOp[]) => Promise<Record<string, string>>`, `renderValue`, `onCopyValue`, `titleExtra`, `keyLabel`, `valueLabel`, `keyPlaceholder`, `valuePlaceholder`
- `frontend/src/pages/NodeDetail.tsx` — uses `isKeyReadOnly`, `validateKey`, `editDisabled`
- `frontend/src/pages/ServiceDetail.tsx` — uses same props as NodeDetail for service labels

- [ ] **Step 2: Rewrite KeyValueEditor internals**

Replace the 322-line implementation with a thin wrapper (~120 lines) around `EditableTable<[string, string]>`.

Key mapping:
- `entries: Record<string, string>` → `items = useMemo(() => Object.entries(entries).sort(([a],[b]) => a.localeCompare(b)), [entries])`
- `columns={[keyLabel ?? "Key", valueLabel ?? "Value"]}` — maps `keyLabel`/`valueLabel` props to `EditableTable`'s `columns`
- Internal state: only `newKey`, `newValue` (editing/saving/draft/adding managed by EditableTable)
- `renderKeyCell(item)`: `item[0]` (key text) + read-only badge when `isKeyReadOnly?.(item[0])`
- `renderValueCell(item, index, update)`: `Input` when editable (onChange calls `update([item[0], event.target.value])`), plain text when `isKeyReadOnly`
- `renderAddKeyCell`: `Input` for new key with autoFocus
- `renderAddValueCell`: `Input` for new value, Enter commits via `onAddCommit`
- `renderAddError`: returns `newKeyError` text span or null
- `canAdd`: `newKey.trim() !== "" && !newKeyError`
- `onAddCommit`: returns `[newKey.trim(), newValue] as [string, string]` or null if empty
- `onAddReset`: clears `newKey` and `newValue`
- `onSave`: **must `await`** the original prop. Implementation: `async (items: [string, string][]) => { const ops = diffToPatchOps(items, entries, isKeyReadOnly); if (ops.length === 0) return; await onSave(ops); }` — this ensures the original prop's internal state setters (e.g. `setNodeLabels(updated)` in NodeDetail) fire before `EditableTable` closes the editor.
- `renderReadOnly(items)`: `<KeyValuePills entries={items} renderValue={renderValue} onCopy={onCopyValue} />`
- `emptyLabel`: `"No " + title.toLowerCase()`
- `emptyHint`: `"Click Edit to add entries."`

The `KeyValueEditorProps` interface stays **exactly the same**.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 4: Run frontend lint**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint`
Expected: Success

- [ ] **Step 5: Verify the app works**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run fmt`

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/KeyValueEditor.tsx
git commit -m "refactor: rewrite KeyValueEditor as thin wrapper around EditableTable"
```

---

### Task 3: Refactor Attachment Editors to Use EditableTable

**Files:**
- Modify: `frontend/src/components/service-detail/ConfigsEditor.tsx`
- Modify: `frontend/src/components/service-detail/SecretsEditor.tsx`
- Modify: `frontend/src/components/service-detail/NetworksEditor.tsx`

- [ ] **Step 1: Rewrite ConfigsEditor**

Replace the ~300-line implementation with a wrapper (~80 lines) around `EditableTable<ServiceConfigRef>`.

The component keeps: `availableConfigs` state + useEffect fetch, `newConfigId`, `newTargetPath`, `handleConfigSelected` (auto-fill).

Passes to EditableTable:
- `items={configs}`, `columns={["Config", "Target"]}`, `title="Configs"`
- `editDisabled={!canEdit}`, `defaultOpen={configs.length > 0}`
- `keyFn={(item) => item.configID}`
- `renderKeyCell={(item) => <span className="font-mono text-xs">{item.configName}</span>}`
- `renderValueCell={(item, index, update) => <Input value={item.fileName} onChange={...update({...item, fileName})} className="font-mono text-xs" />}`
- `renderAddKeyCell={(draft) => <Combobox ... />}` — with availableConfigs options
- `renderAddValueCell={() => <Input ... />}` — target path input
- `canAdd={!!newConfigId && !!newTargetPath}`
- `onAddCommit`: returns `{ configID, configName, fileName }` or null
- `onAddReset`: clears `newConfigId` and `newTargetPath`
- `onSave`: calls `api.patchServiceConfigs(serviceId, items)`, then `onSaved(result.configs)`
- `renderReadOnly`: `SimpleTable` with linked config names (same JSX as current read-only view)
- `emptyLabel="No configs attached"`, `emptyHint="Click Edit to attach Docker configs to this service."`

- [ ] **Step 2: Rewrite SecretsEditor**

Same pattern as ConfigsEditor with differences:
- `ServiceSecretRef` type, `secretID`/`secretName`/`fileName` fields
- Default path: `/run/secrets/<name>`
- API: `api.secrets()`, `api.patchServiceSecrets()`
- Links: `/secrets/${secretID}`

- [ ] **Step 3: Rewrite NetworksEditor**

Same pattern but:
- Extra prop: `networkNames: Record<string, string>`
- `renderKeyCell`: uses `nameMap[target] || target` (merged from props + fetched)
- `renderValueCell`: `MultiCombobox` for editable aliases
- `renderAddKeyCell(draft)`: Combobox with filtered options (exclude `draft` targets)
- `renderAddValueCell`: `MultiCombobox` with `options={[]}`
- `canAdd={!!newNetworkId}` (aliases optional)
- `onAddCommit`: returns `{ target, aliases }` or null

- [ ] **Step 4: Verify TypeScript compiles**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npx tsc -b --noEmit`
Expected: Success

- [ ] **Step 5: Run lint and format**

Run: `cd /Users/moritz/GolandProjects/cetacean/frontend && npm run lint && npm run fmt`
Expected: Success

- [ ] **Step 6: Run full make check**

Run: `cd /Users/moritz/GolandProjects/cetacean && make check`
Expected: All checks pass

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/service-detail/ConfigsEditor.tsx frontend/src/components/service-detail/SecretsEditor.tsx frontend/src/components/service-detail/NetworksEditor.tsx
git commit -m "refactor: rewrite attachment editors as thin wrappers around EditableTable"
```
