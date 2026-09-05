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

/**
 * The resource type this widget lists.
 *
 * A host renders a widget for a tool result, so the type is whatever the model
 * asked find for. The host does not hand the widget the call's arguments, so
 * the widget reads them from its own URL fragment — which is how one bundle
 * serves every resource type — and lists services by default.
 */
function resourceTypeFromLocation(): string {
  const fromHash = new URLSearchParams(window.location.hash.replace(/^#/, "")).get("type");

  return fromHash ?? "services";
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
  const { isConnected, error: connectionError } = host;

  const resourceType = resourceTypeFromLocation();
  const { data, error, isLoading } = useToolData<FindResult>(host, "find", {
    type: resourceType,
  });

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (error) {
    return <Message text={error.message} />;
  }

  if (isLoading || !data) {
    return <Message text={`Loading ${resourceType}…`} />;
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
