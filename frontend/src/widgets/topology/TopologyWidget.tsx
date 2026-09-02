import { useCetaceanHost, useToolData } from "../bridge";
import type { TopologyGraphData, TopologyView } from "./layout";
import { TopologyGraph } from "./TopologyGraph";
import { useState } from "react";

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
  const { error: connectionError, isConnected } = host;
  const [view, setView] = useState<TopologyView>("network");

  const { data, error, isLoading } = useToolData<TopologyGraphData>(host, "get_topology", { view });

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
    return <Message text={`Loading the ${view} topology…`} />;
  }

  return (
    <TopologyGraph
      graph={data}
      onViewChange={setView}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}
