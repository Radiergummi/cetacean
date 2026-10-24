import { useCetaceanHost, useToolData } from "../bridge";
import type { TopologyGraphData, TopologyView } from "./layout";
import { TopologyGraph } from "./TopologyGraph";
import { useState } from "react";

/**
 * The view and node the host called get_topology with: the graph to draw first.
 * Only drain-impact reads the node, and it cannot be drawn without one.
 */
function topologyArgumentsFrom(
  input: Record<string, unknown> | undefined,
): { view: string; node?: string } | undefined {
  if (!input) {
    return undefined;
  }

  const view = typeof input.view === "string" && input.view !== "" ? input.view : "network";
  const node = input.node;

  return typeof node === "string" && node !== "" ? { view, node } : { view };
}

/**
 * Renders the cluster topology as an interactive graph.
 *
 * The view is widget state, not host state: switching it re-calls get_topology
 * through the host, so the second view is fetched under the same identity and
 * the same ACL grants as the first. Everything a viewer can pan around is
 * therefore something the calling identity may read.
 */
export function TopologyWidget() {
  const host = useCetaceanHost();
  const { error: connectionError, isConnected, toolInput } = host;
  const [chosenView, setChosenView] = useState<TopologyView | undefined>(undefined);

  const called = topologyArgumentsFrom(toolInput);
  const args = called && chosenView ? { ...called, view: chosenView } : called;
  const { data, error, isLoading } = useToolData<TopologyGraphData>(host, "get_topology", args);

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (!args) {
    return <Message text="Waiting for the host to say which topology to draw…" />;
  }

  if (error) {
    return <Message text={error.message} />;
  }

  if (isLoading || !data) {
    return <Message text={`Loading the ${args.view} topology…`} />;
  }

  return (
    <TopologyGraph
      graph={data}
      onViewChange={setChosenView}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}
