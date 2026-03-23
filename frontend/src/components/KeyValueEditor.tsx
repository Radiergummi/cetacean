import type { PatchOp } from "@/api/types";
import KeyValuePills from "@/components/data/KeyValuePills";
import { EditableTable } from "@/components/EditableTable";
import { Input } from "@/components/ui/input";
import type React from "react";
import { useMemo, useState } from "react";

interface KeyValueEditorProps {
  title: string;
  /** Optional extra element rendered inline after the section title (e.g. a help link). */
  titleExtra?: React.ReactNode;
  entries: Record<string, string>;
  keyLabel?: string;
  valueLabel?: string;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  onSave: (ops: PatchOp[]) => Promise<Record<string, string>>;
  defaultOpen?: boolean;
  renderValue?: (value: string) => React.ReactNode;
  onCopyValue?: React.ClipboardEventHandler;
  editDisabled?: boolean;
  /** Return true if this key should be read-only (no edit, no delete). */
  isKeyReadOnly?: (key: string) => boolean;
  /** Validate a new key. Return an error message, or null if valid. */
  validateKey?: (key: string) => string | null;
}

export function KeyValueEditor({
  title,
  titleExtra,
  entries,
  keyLabel = "Key",
  valueLabel = "Value",
  keyPlaceholder = "key",
  valuePlaceholder = "value",
  onSave,
  defaultOpen = false,
  renderValue,
  onCopyValue,
  editDisabled = false,
  isKeyReadOnly,
  validateKey,
}: KeyValueEditorProps) {
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");

  const items = useMemo(
    () => Object.entries(entries).sort(([a], [b]) => a.localeCompare(b)),
    [entries],
  );

  const newKeyError = newKey.trim() ? (validateKey?.(newKey.trim()) ?? null) : null;

  return (
    <EditableTable<[string, string]>
      title={title}
      titleExtra={titleExtra}
      items={items}
      columns={[keyLabel, valueLabel]}
      defaultOpen={defaultOpen}
      editDisabled={editDisabled}
      emptyLabel={`No ${title.toLowerCase()}`}
      emptyHint="Click Edit to add entries."
      keyFn={([key]) => key}
      renderReadOnly={(sortedItems) => (
        <KeyValuePills
          entries={sortedItems}
          renderValue={renderValue}
          onCopy={onCopyValue}
        />
      )}
      canRemove={isKeyReadOnly ? ([key]) => !isKeyReadOnly(key) : undefined}
      renderKeyCell={([key]) => (
        <span className="font-mono text-xs">
          {key}
          {isKeyReadOnly?.(key) && (
            <span className="ml-2 rounded bg-muted px-1.5 py-0.5 font-sans text-[10px] text-muted-foreground">
              read-only
            </span>
          )}
        </span>
      )}
      renderValueCell={([key, value], _index, update) => {
        const readOnly = isKeyReadOnly?.(key) ?? false;

        return readOnly ? (
          <span className="font-mono text-xs text-muted-foreground">{value}</span>
        ) : (
          <Input
            value={value}
            onChange={(event) => update([key, event.target.value])}
            className="font-mono text-xs"
          />
        );
      }}
      renderAddKeyCell={() => (
        <Input
          value={newKey}
          onChange={(event) => setNewKey(event.target.value)}
          placeholder={keyPlaceholder}
          className="font-mono text-xs"
          autoFocus
        />
      )}
      renderAddValueCell={() => (
        <Input
          value={newValue}
          onChange={(event) => setNewValue(event.target.value)}
          placeholder={valuePlaceholder}
          className="font-mono text-xs"
        />
      )}
      renderAddError={() => (newKeyError ? <span>{newKeyError}</span> : null)}
      canAdd={!!newKey.trim() && !newKeyError}
      onAddCommit={() => {
        const key = newKey.trim();

        if (!key || newKeyError) {
          return null;
        }

        return [key, newValue] as [string, string];
      }}
      onAddReset={() => {
        setNewKey("");
        setNewValue("");
      }}
      onSave={async (draftItems) => {
        const ops: PatchOp[] = [];
        const draftMap = Object.fromEntries(draftItems);

        for (const key of Object.keys(entries)) {
          if (isKeyReadOnly?.(key)) {
            continue;
          }

          if (!(key in draftMap)) {
            ops.push({ op: "remove", path: `/${key}` });
          }
        }

        for (const [key, value] of draftItems) {
          if (isKeyReadOnly?.(key)) {
            continue;
          }

          if (!(key in entries)) {
            ops.push({ op: "add", path: `/${key}`, value });
          } else if (entries[key] !== value) {
            ops.push({ op: "replace", path: `/${key}`, value });
          }
        }

        if (ops.length === 0) {
          return;
        }

        await onSave(ops);
      }}
    />
  );
}
