import { useCetaceanHost, useToolData } from "../bridge";
import { MetricsView } from "./MetricsView";
import type { MetricsResult } from "./types";
import { useState } from "react";

/**
 * The call the host rendered this widget for. Without a target there is
 * nothing to chart, so a widget that never receives one says so rather than
 * guessing; every target but the cluster also needs an id.
 */
interface MetricsArguments {
  target: string;
  id?: string | undefined;
  metric: string;
  top?: number | undefined;
  by?: string | undefined;
  range?: string | undefined;
}

function metricsArgumentsFrom(
  input: Record<string, unknown> | undefined,
): MetricsArguments | undefined {
  const target = input?.target;
  const id = typeof input?.id === "string" && input.id !== "" ? input.id : undefined;

  if (typeof target !== "string" || target === "" || (target !== "cluster" && !id)) {
    return undefined;
  }

  const { by, metric, range, top } = input ?? {};
  const args: MetricsArguments = { target, metric: typeof metric === "string" ? metric : "cpu" };

  if (id) {
    args.id = id;
  }

  if (typeof top === "number") {
    args.top = top;
  }

  if (typeof by === "string") {
    args.by = by;
  }

  if (typeof range === "string") {
    args.range = range;
  }

  return args;
}

/**
 * Charts one metric for a service, a node, or a ranking across either.
 *
 * The range is widget state: picking a new one re-calls get_metrics through the
 * host, so the wider window is fetched under the same identity and the same ACL
 * grants as the first — a widget cannot widen its own reach by asking for more.
 */
export function MetricsWidget() {
  const host = useCetaceanHost();
  const { error: connectionError, isConnected, toolInput } = host;
  const [pickedRange, setRange] = useState<string | undefined>();

  const args = metricsArgumentsFrom(toolInput);
  const { data, error, isLoading } = useToolData<MetricsResult>(
    host,
    "get_metrics",
    args && { ...args, range: pickedRange ?? args.range ?? "1h" },
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
    return <Message text={`Loading ${args.metric} for ${args.id ?? "the cluster"}…`} />;
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
