/**
 * Column definitions for each resource type the `find` tool can return.
 *
 * `find` returns compact rows (`internal/cluster.Row`), not raw Docker Engine
 * API objects, so every field lives under the same lower-cased keys for every
 * resource type: `id`, `name`, `state`, `detail`, `stack`, and — only where a
 * replica count means something — `desired`/`running`. What varies per type is
 * only which of those columns to show and what to call them: `detail` is a
 * service's image, a node's role, or a network's/volume's driver; `desired` is
 * a stack's service count rather than a replica target.
 */

/** A row from `find`. Every resource type shares this shape. */
export interface ResourceRecord {
  id: string;
  name: string;
  type: string;
  stack?: string;
  state?: string;
  detail?: string;
  desired?: number;
  running?: number;
}

export interface WidgetColumn {
  key: string;
  header: string;
  /** Reads the display value. Returning "" renders an em dash. */
  value: (record: ResourceRecord) => string;
}

/**
 * Renders a field as a display string, tolerating an absent value.
 */
function field(key: keyof ResourceRecord): (record: ResourceRecord) => string {
  return (record) => {
    const found = record[key];

    if (found === undefined || found === null) {
      return "";
    }

    return String(found);
  };
}

const name: WidgetColumn = { key: "name", header: "Name", value: field("name") };
const identifier: WidgetColumn = { key: "id", header: "ID", value: field("id") };
const state: WidgetColumn = { key: "state", header: "State", value: field("state") };
const stack: WidgetColumn = { key: "stack", header: "Stack", value: field("stack") };
const running: WidgetColumn = { key: "running", header: "Running", value: field("running") };

function detailColumn(header: string): WidgetColumn {
  return { key: "detail", header, value: field("detail") };
}

function desiredColumn(header: string): WidgetColumn {
  return { key: "desired", header, value: field("desired") };
}

const columnsByType: Record<string, WidgetColumn[]> = {
  services: [
    name,
    stack,
    state,
    detailColumn("Image"),
    desiredColumn("Desired"),
    running,
    identifier,
  ],
  nodes: [name, state, detailColumn("Role"), identifier],
  tasks: [name, state, detailColumn("Node"), identifier],
  stacks: [name, desiredColumn("Services"), identifier],
  configs: [name, stack, identifier],
  secrets: [name, stack, identifier],
  networks: [name, stack, detailColumn("Driver"), identifier],
  volumes: [name, stack, detailColumn("Driver"), identifier],
};

/**
 * Columns for a resource type. Every type falls through to the same universal
 * columns when `columnsByType` has not been taught about it, since a compact
 * row from an unknown type still carries `name`/`state`/`detail`/`id`.
 */
export function columnsFor(resourceType: string): WidgetColumn[] {
  return columnsByType[resourceType] ?? [name, state, detailColumn("Detail"), identifier];
}
