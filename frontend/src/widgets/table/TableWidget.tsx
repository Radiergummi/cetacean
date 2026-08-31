import { useCetaceanHost, useToolData } from "../bridge";

/**
 * A single hit from the `search` tool, mirroring cluster.SearchResult.
 */
interface SearchHit {
  type: string;
  id: string;
  name: string;
  detail?: string;
  state?: string;
}

/**
 * The `search` tool's structured output, mirroring cluster.SearchResults.
 *
 * The outer keys are capitalised because the Go struct carries no JSON tags —
 * this is the MCP tool's shape, which is *not* the REST API's `SearchResponse`
 * (`results`/`counts`/`total`). Widgets read the tool, so they follow the tool.
 */
interface SearchResults {
  Hits: Record<string, SearchHit[]>;
  Counts: Record<string, number>;
  Total: number;
}

/**
 * Renders the cluster's services as a table inside an MCP Apps host.
 *
 * Phase 11 establishes the path end to end — build, embed, ui:// resource, host
 * bridge, tool call — with the plainest rendering that proves data arrives.
 * Phase 12 replaces the markup here with the dashboard's DataTable, so there is
 * one implementation of the view rather than two.
 */
export function TableWidget() {
  const { isConnected, error: connectionError } = useCetaceanHost();

  // An empty query matches every resource, which is what a table wants as its
  // initial state; Phase 12 adds the search box that narrows it.
  const { data, error, isLoading } = useToolData<SearchResults>("search", { query: "" });

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
    return <Message text="Loading cluster resources…" />;
  }

  const services = data.Hits?.services ?? [];

  if (services.length === 0) {
    return <Message text="No services in this cluster." />;
  }

  return (
    <table className="w-full border-collapse text-sm">
      <thead>
        <tr className="border-b text-left">
          <th className="p-2 font-medium">Service</th>
          <th className="p-2 font-medium">Detail</th>
          <th className="p-2 font-medium">State</th>
        </tr>
      </thead>
      <tbody>
        {services.map(({ id, name, detail, state }) => (
          <tr
            key={id}
            className="border-b last:border-0"
          >
            <td className="p-2">{name}</td>
            <td className="p-2 font-mono text-xs">{detail}</td>
            <td className="p-2">{state}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}
