/**
 * Column definitions for each resource type list_resources can return.
 *
 * The records are the Docker Engine API types verbatim — Cetacean keeps no
 * domain models of its own — so the accessors read capitalised Docker fields
 * (`Spec.Name`, `Description.Hostname`) rather than the lower-cased shapes the
 * REST API's own view types use.
 *
 * Everything is optional-safe: a widget renders whatever the server sent, and a
 * host may be pointed at a cluster whose Docker version omits a field.
 */

/** A record from list_resources. Shape varies by resource type. */
export type ResourceRecord = Record<string, unknown>;

export interface WidgetColumn {
  key: string;
  header: string;
  /** Reads the display value. Returning "" renders an em dash. */
  value: (record: ResourceRecord) => string;
}

/**
 * Reads a dotted path out of a record, tolerating missing intermediate objects.
 */
function path(record: ResourceRecord, dotted: string): unknown {
  return dotted.split(".").reduce<unknown>((current, segment) => {
    if (current === null || typeof current !== "object") {
      return undefined;
    }

    return (current as Record<string, unknown>)[segment];
  }, record);
}

/**
 * Reads a dotted path and renders it as a display string.
 */
function text(dotted: string): (record: ResourceRecord) => string {
  return (record) => {
    const found = path(record, dotted);

    if (found === undefined || found === null) {
      return "";
    }

    if (typeof found === "string" || typeof found === "number" || typeof found === "boolean") {
      return String(found);
    }

    return "";
  };
}

const identifier: WidgetColumn = { key: "id", header: "ID", value: text("ID") };

const columnsByType: Record<string, WidgetColumn[]> = {
  services: [
    { key: "name", header: "Name", value: text("Spec.Name") },
    { key: "image", header: "Image", value: text("Spec.TaskTemplate.ContainerSpec.Image") },
    identifier,
  ],
  nodes: [
    { key: "hostname", header: "Hostname", value: text("Description.Hostname") },
    { key: "role", header: "Role", value: text("Spec.Role") },
    { key: "availability", header: "Availability", value: text("Spec.Availability") },
    { key: "state", header: "State", value: text("Status.State") },
  ],
  tasks: [
    { key: "service", header: "Service", value: text("ServiceName") },
    { key: "node", header: "Node", value: text("NodeHostname") },
    { key: "state", header: "State", value: text("Status.State") },
    identifier,
  ],
  stacks: [
    { key: "name", header: "Name", value: text("Name") },
    {
      key: "services",
      header: "Services",
      value: (record) => {
        const services = record["Services"];

        return Array.isArray(services) ? String(services.length) : "";
      },
    },
  ],
  configs: [
    { key: "name", header: "Name", value: text("Spec.Name") },
    { key: "created", header: "Created", value: text("CreatedAt") },
    identifier,
  ],
  secrets: [
    { key: "name", header: "Name", value: text("Spec.Name") },
    { key: "created", header: "Created", value: text("CreatedAt") },
    identifier,
  ],
  networks: [
    { key: "name", header: "Name", value: text("Name") },
    { key: "driver", header: "Driver", value: text("Driver") },
    { key: "scope", header: "Scope", value: text("Scope") },
    identifier,
  ],
  volumes: [
    { key: "name", header: "Name", value: text("Name") },
    { key: "driver", header: "Driver", value: text("Driver") },
    { key: "mountpoint", header: "Mount point", value: text("Mountpoint") },
  ],
};

/**
 * Columns for a resource type, falling back to a name/ID pair for a type this
 * widget has not been taught about — a new server type renders plainly rather
 * than as an empty table.
 */
export function columnsFor(resourceType: string): WidgetColumn[] {
  return (
    columnsByType[resourceType] ?? [
      { key: "name", header: "Name", value: text("Spec.Name") },
      identifier,
    ]
  );
}
