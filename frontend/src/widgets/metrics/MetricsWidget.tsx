import { useCetaceanHost, useToolData } from "../bridge";
import { MetricsView } from "./MetricsView";
import type { MetricsResult } from "./types";
import { useState } from "react";

/**
 * The call the host rendered this widget for: which resource, and which metric.
 * Without a target there is nothing to chart, so a widget that never receives
 * one says so rather than guessing at a service.
 */
interface MetricsArguments {
  target: string;
  id: string;
  metric: string;
}

function metricsArgumentsFrom(
  input: Record<string, unknown> | undefined,
): MetricsArguments | undefined {
  const target = input?.target;
  const id = input?.id;

  if (typeof target !== "string" || typeof id !== "string" || target === "" || id === "") {
    return undefined;
  }

  const metric = input?.metric;

  return { target, id, metric: typeof metric === "string" ? metric : "cpu" };
}

/**
 * Charts one metric for one service or node.
 *
 * The range is widget state: picking a new one re-calls get_metrics through the
 * host, so the wider window is fetched under the same identity and the same ACL
 * grants as the first — a widget cannot widen its own reach by asking for more.
 */
export function MetricsWidget() {
  const host = useCetaceanHost();
  const { error: connectionError, isConnected, toolInput } = host;
  const [range, setRange] = useState("1h");

  const args = metricsArgumentsFrom(toolInput);
  const { data, error, isLoading } = useToolData<MetricsResult>(
    host,
    "get_metrics",
    args ? { ...args, range } : {},
  );

  if (connectionError) {
    return <Message text={`Could not reach the host: ${connectionError.message}`} />;
  }

  if (!isConnected) {
    return <Message text="Connecting to the host…" />;
  }

  if (!args) {
    return <Message text="The host has not said what to measure." />;
  }

  if (error) {
    return <Message text={error.message} />;
  }

  if (isLoading || !data) {
    return <Message text={`Loading ${args.metric} for ${args.id}…`} />;
  }

  return (
    <MetricsView
      result={data}
      onRangeChange={setRange}
    />
  );
}

function Message({ text }: { text: string }) {
  return <p className="p-3 text-sm opacity-70">{text}</p>;
}
