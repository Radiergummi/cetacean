# EditableTable — Shared List Editor Component

## Summary

Extract a generic `EditableTable<T>` component that manages the full edit lifecycle (editing toggle, draft state, add-row lifecycle, save/cancel, escape cancel) for any two-column editable list. Refactor `KeyValueEditor` and the three attachment editors (ConfigsEditor, SecretsEditor, NetworksEditor) to use it, eliminating ~600 lines of duplicated shell code.

## Motivation

`KeyValueEditor`, `ConfigsEditor`, `SecretsEditor`, and `NetworksEditor` all implement the same structural pattern: CollapsibleSection with Edit button → bordered table with editable rows → "Add another" in footer → Save/Cancel. The state management (editing, saving, adding, draft, escape cancel, error display) is identical. Only the cell rendering and save mechanism differ.

## Design

### EditableTable Component

**File:** `frontend/src/components/EditableTable.tsx`

Generic component `EditableTable<T>` that owns all edit lifecycle state.

**Props:**
```ts
interface EditableTableProps<T> {
  title: string;
  titleExtra?: ReactNode;
  items: T[];
  columns: [string, string];
  defaultOpen?: boolean;
  editDisabled?: boolean;

  // Read-only view
  renderReadOnly: (items: T[]) => ReactNode;
  emptyLabel: string;
  emptyHint?: string;

  // Edit mode — existing rows
  keyFn: (item: T, index: number) => string | number;
  renderKeyCell: (item: T, index: number) => ReactNode;
  renderValueCell: (item: T, index: number, update: (next: T) => void) => ReactNode;

  // Edit mode — add row
  renderAddKeyCell: (draft: T[]) => ReactNode;
  renderAddValueCell: (draft: T[]) => ReactNode;
  renderAddError?: () => ReactNode;
  canAdd: boolean;
  onAddCommit: () => T | null;
  onAddReset: () => void;

  // Save
  onSave: (items: T[]) => Promise<void>;
}
```

**Internal state:** `editing: boolean`, `saving: boolean`, `saveError: string | null`, `draft: T[]`, `adding: boolean`

**Lifecycle:**
- `openEdit()`: copies `items` to `draft` (shallow spread per item), sets `adding = items.length === 0`
- `cancelEdit()`: exits edit mode, clears error
- `removeRow(index)`: filters from draft by index
- "Add another" click: if `adding && canAdd`, calls `onAddCommit()`. If it returns `null`, keeps the add row open (no-op). If it returns `T`, appends to draft, calls `onAddReset()`, keeps `adding = true` for next row. If `adding && !canAdd`, button is disabled.
- `save()`: if `adding && canAdd`, calls `onAddCommit()` to include pending row in effective draft (if `onAddCommit` returns null, saves without the pending row). If `adding && !canAdd`, the pending add row is silently ignored — save proceeds with the committed draft only (matching current `KeyValueEditor` behavior). Calls `onSave(effectiveDraft)`. On success, sets `editing = false`. On error, sets `saveError`.
- Escape: `useEscapeCancel(editing, cancelEdit)`

**Rendered structure (edit mode):**
```
CollapsibleSection
  └─ div.flex.flex-col.gap-3
       └─ div.overflow-x-auto.rounded-lg.border
            ├─ table.w-full.min-w-max.whitespace-nowrap
            │    ├─ thead (columns[0], columns[1], empty 12-wide th)
            │    └─ tbody
            │         ├─ per-item rows: renderKeyCell + renderValueCell + ghost delete button
            │         ├─ add row (when adding): renderAddKeyCell(draft) + renderAddValueCell(draft)
            │         └─ add error row (when adding && renderAddError returns non-null): full-width colSpan=3
            ├─ saveError paragraph
            └─ footer: "Add another" | Save + Cancel
```

**Rendered structure (read-only):**
- If `items.length > 0`: calls `renderReadOnly(items)`
- If `items.length === 0`: empty state div with `emptyLabel` + optional `emptyHint` (shown when `!editDisabled`)

**Edit button:** shown when `!editing && !editDisabled`, rendered in `controls` prop of CollapsibleSection. When `editing && titleExtra`, shows `titleExtra` in controls instead.

All class names match the existing `KeyValueEditor` exactly.

### KeyValueEditor Refactored

**File:** `frontend/src/components/KeyValueEditor.tsx`

Refactored to wrap `EditableTable<[string, string]>`. The `KeyValueEditor` API stays **exactly the same** — no consumer changes needed.

**Internal mapping:**
- `entries: Record<string, string>` → `items: [string, string][]` (sorted by key)
- `renderKeyCell`: shows key text + "read-only" badge when `isKeyReadOnly`
- `renderValueCell`: shows `Input` when editable, plain text when read-only
- `renderAddKeyCell`: `(draft) =>` `Input` for new key
- `renderAddValueCell`: `(draft) =>` `Input` for new value, Enter commits
- `renderAddError`: `() =>` validation error text when `newKeyError` is set (rendered as full-width colSpan=3 row)
- `onAddCommit`: returns `[newKey.trim(), newValue]` or null if key is empty/invalid
- `onSave`: receives `[string, string][]`, diffs against original `entries` to produce `PatchOp[]`, calls the original `onSave(ops)` prop
- `renderReadOnly`: renders `KeyValuePills`
- `canAdd`: `newKey.trim() !== "" && !newKeyError`

All existing props (`isKeyReadOnly`, `validateKey`, `renderValue`, `onCopyValue`, `titleExtra`) continue to work:
- `renderValue` and `onCopyValue` are captured in the `renderReadOnly` closure: `(items) => <KeyValuePills entries={items} renderValue={renderValue} onCopy={onCopyValue} />`
- `isKeyReadOnly` is used in `renderKeyCell` (badge) and `renderValueCell` (read-only vs Input)
- `validateKey` feeds into `canAdd` and `renderAddError`
- `titleExtra` is passed through to `EditableTable`

### Attachment Editors Refactored

Each becomes a thin wrapper (~50-80 lines) around `EditableTable<ServiceConfigRef>` etc.

**ConfigsEditor:** manages `newConfigId`, `newTargetPath`, `availableConfigs` state + useEffect fetch. Passes:
- `renderKeyCell`: plain text config name
- `renderValueCell`: `Input` for fileName, `onChange` calls `update({...item, fileName: value})`
- `renderAddKeyCell`: `Combobox` for config selection (auto-fills target path)
- `renderAddValueCell`: `Input` for target path
- `canAdd`: `!!newConfigId && !!newTargetPath`
- `onAddCommit`: returns `{ configID, configName, fileName }`
- `onSave`: calls `api.patchServiceConfigs`, then `onSaved`
- `renderReadOnly`: `SimpleTable` with linked config names

**SecretsEditor:** same pattern, different default path (`/run/secrets/<name>`), different API calls.

**NetworksEditor:** same pattern but:
- `renderValueCell`: `MultiCombobox` for aliases (editable in existing rows)
- `renderAddValueCell`: `MultiCombobox` with `options={[]}`
- `renderAddKeyCell(draft)`: Combobox filters out network IDs already in `draft`
- `canAdd`: `!!newNetworkId` (aliases optional)

## Testing

- `EditableTable` unit tests: renders read-only, opens edit, adds/removes rows, saves, handles errors, escape cancels
- Existing `KeyValueEditor` behavior unchanged — run any existing tests
- Attachment editors: verify existing behavior preserved (combobox picker, auto-fill, filtered networks)

## Files Changed

- Create: `frontend/src/components/EditableTable.tsx`
- Modify: `frontend/src/components/KeyValueEditor.tsx` (refactor internals, keep API)
- Modify: `frontend/src/components/service-detail/ConfigsEditor.tsx` (simplify to wrapper)
- Modify: `frontend/src/components/service-detail/SecretsEditor.tsx` (simplify to wrapper)
- Modify: `frontend/src/components/service-detail/NetworksEditor.tsx` (simplify to wrapper)
