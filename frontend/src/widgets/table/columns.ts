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

/**
 * A column reading one row key under a given header.
 *
 * Every column is the same shape — a row carries `id`, `name`, `state`,
 * `detail`, `stack`, `desired` and `running` under exactly those names — so
 * the only thing that varies per type is which keys it shows and what each
 * header is called. `detail` and `desired` are why the header is a parameter
 * rather than derived: the same key is the image on a service, the role on a
 * node and the driver on a network.
 */
function column(key: keyof ResourceRecord, header: string): WidgetColumn {
  return { key: String(key), header, value: field(key) };
}

const columnsByType: Record<string, WidgetColumn[]> = {
  services: [
    column("name", "Name"),
    column("stack", "Stack"),
    column("state", "State"),
    column("detail", "Image"),
    column("desired", "Desired"),
    column("running", "Running"),
    column("id", "ID"),
  ],
  nodes: [
    column("name", "Name"),
    column("state", "State"),
    column("detail", "Role"),
    column("id", "ID"),
  ],
  tasks: [
    column("name", "Name"),
    column("state", "State"),
    column("detail", "Node"),
    column("id", "ID"),
  ],
  stacks: [column("name", "Name"), column("desired", "Services"), column("id", "ID")],
  configs: [column("name", "Name"), column("stack", "Stack"), column("id", "ID")],
  secrets: [column("name", "Name"), column("stack", "Stack"), column("id", "ID")],
  networks: [
    column("name", "Name"),
    column("stack", "Stack"),
    column("detail", "Driver"),
    column("id", "ID"),
  ],
  volumes: [
    column("name", "Name"),
    column("stack", "Stack"),
    column("detail", "Driver"),
    column("id", "ID"),
  ],
};

/**
 * Columns for a resource type. Every type falls through to the same universal
 * columns when `columnsByType` has not been taught about it, since a compact
 * row from an unknown type still carries `name`/`state`/`detail`/`id`.
 */
export function columnsFor(resourceType: string): WidgetColumn[] {
  return (
    columnsByType[resourceType] ?? [
      column("name", "Name"),
      column("state", "State"),
      column("detail", "Detail"),
      column("id", "ID"),
    ]
  );
}
