import { useCetaceanHost, useToolData } from "../bridge";
import type { ResourceRecord } from "./columns";
import { ResourceTable } from "./ResourceTable";

/**
 * The `find` tool's structured output, mirroring internal/mcp.findResult.
 * Total is the count before paging.
 */
interface FindResult {
  type: string;
  items: ResourceRecord[];
  total: number;
}

/** The `find` arguments the table repeats; `raw` would only add records it never renders. */
const findArgumentNames = [
  "type",
  "query",
  "state",
  "stack",
  "node",
  "image",
  "label",
  "limit",
  "offset",
] as const;

/**
 * The arguments the host called `find` with. One bundle serves every resource
 * type, and nothing but the tool input says which one this call listed.
 */
function findArgumentsFrom(
  input: Record<string, unknown> | undefined,
): Record<string, unknown> | undefined {
  if (!input) {
    return undefined;
  }

  return Object.fromEntries(
    findArgumentNames.flatMap((name) => (input[name] === undefined ? [] : [[name, input[name]]])),
  );
}

/**
 * Renders a cluster resource listing as a searchable, sortable table.
 *
 * Data comes from Cetacean's own `find` tool through the host, never from its
 * HTTP API, so the rows are exactly what the calling identity's ACL grants
 * allow.
 */
export function TableWidget() {
  const host = useCetaceanHost();
  const { isConnected, error: connectionError, toolInput } = host;

  const args = findArgumentsFrom(toolInput);
  const { data, error, isLoading } = useToolData<FindResult>(host, "find", args);

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (!args) {
    return <Message text="Waiting for the host to say what to list…" />;
  }

  if (error) {
    return <Message text={error.message} />;
  }

  if (isLoading || !data) {
    return <Message text={`Loading ${typeof args.type === "string" ? args.type : "matches"}…`} />;
  }

  return (
    <ResourceTable
      resourceType={data.type}
      records={data.items}
      total={data.total}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}
