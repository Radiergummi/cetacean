import type { MonitoringStatus as Status } from "../../api/types";
import { readStoredValue, writeStoredValue } from "@/lib/storage";
import { AlertTriangle, BarChart3, X } from "lucide-react";
import type React from "react";
import { useState } from "react";

const dismissKey = "cetacean:dismiss-monitoring-banner";

interface Props {
  status: Status;
  source?: "nodeExporter" | "cadvisor" | undefined;
}

export default function MonitoringStatus({ status, source }: Props) {
  const [dismissed, setDismissed] = useState(() => readStoredValue(dismissKey) === "true");

  // Fully healthy — nothing to show
  if (status.prometheusConfigured && status.prometheusReachable) {
    const nodeExporterRunning =
      !status.nodeExporter || status.nodeExporter.targets >= status.nodeExporter.nodes;
    const cAdvisorRunning = !status.cadvisor || status.cadvisor.targets >= status.cadvisor.nodes;

    if (nodeExporterRunning && cAdvisorRunning) {
      return null;
    }
  }

  // State A: Nothing configured (dismissible)
  if (!status.prometheusConfigured) {
    if (dismissed) {
      return null;
    }

    return (
      <Banner
        icon={<BarChart3 className="size-5 shrink-0 text-status-info" />}
        onDismiss={() => {
          writeStoredValue(dismissKey, "true");
          setDismissed(true);
        }}
      >
        <p className="text-sm">
          <strong>Monitoring not configured.</strong> Deploy the monitoring stack to enable CPU,
          memory, and disk metrics across your cluster.
        </p>
        <pre className="mt-2 max-w-fit overflow-x-auto rounded bg-status-info/15 px-2 py-1 text-xs dark:bg-status-info">
          docker stack deploy -c compose.monitoring.yaml cetacean-monitoring
        </pre>
        <p className="mt-3 text-xs">
          Then set{" "}
          <code className="rounded bg-status-info/15 px-1 py-0.5 font-mono dark:bg-status-info">
            prometheus.url
          </code>{" "}
          and restart Cetacean.
        </p>
      </Banner>
    );
  }

  // State B: Prometheus unreachable (not dismissible)
  if (!status.prometheusReachable) {
    return (
      <Banner
        icon={<AlertTriangle className="size-5 shrink-0 text-status-warning" />}
        variant="warn"
      >
        <p className="text-sm">
          <strong>Cannot reach Prometheus</strong> — metrics unavailable. Check that the Prometheus
          service is running and reachable from Cetacean.
        </p>
        {status.error && (
          <pre className="mt-2 max-w-full overflow-x-auto rounded bg-status-warning/15 px-2 py-1 text-xs dark:bg-status-warning">
            {status.error}
          </pre>
        )}
      </Banner>
    );
  }

  // State C: Partial sources
  const hints: string[] = [];

  if (source !== "cadvisor" && status.nodeExporter) {
    const { targets, nodes } = status.nodeExporter;

    if (targets === 0) {
      hints.push("node-exporter not detected — node metrics (CPU, memory, disk) unavailable.");
    } else if (targets < nodes) {
      hints.push(`node-exporter reporting on ${targets} of ${nodes} nodes.`);
    }
  }

  if (source !== "nodeExporter" && status.cadvisor) {
    const { targets, nodes } = status.cadvisor;

    if (targets === 0) {
      hints.push("cAdvisor not detected — container metrics (service CPU/memory) unavailable.");
    } else if (targets < nodes) {
      hints.push(`cAdvisor reporting on ${targets} of ${nodes} nodes.`);
    }
  }

  if (hints.length === 0 || dismissed) {
    return null;
  }

  return (
    <Banner
      icon={<BarChart3 className="size-5 shrink-0 text-status-info" />}
      onDismiss={() => {
        writeStoredValue(dismissKey, "true");
        setDismissed(true);
      }}
    >
      <p className="text-sm">
        <strong>Monitoring partially configured</strong>
      </p>
      <ul className="mt-1 list-inside list-disc space-y-0.5 text-sm">
        {hints.map((hint) => (
          <li key={hint}>{hint}</li>
        ))}
      </ul>
    </Banner>
  );
}

function Banner({
  icon,
  variant = "info",
  onDismiss,
  children,
}: {
  icon: React.ReactNode;
  variant?: "info" | "warn" | undefined;
  onDismiss?: (() => void) | undefined;
  children: React.ReactNode;
}) {
  return (
    <div
      data-variant={variant}
      className="group mb-4 flex items-start gap-3 rounded-lg border border-status-info/30 bg-status-info/10 px-4 py-3 data-[variant=warn]:border-status-warning/30 data-[variant=warn]:bg-status-warning/10 dark:border-status-info dark:bg-status-info dark:data-[variant=warn]:border-status-warning dark:data-[variant=warn]:bg-status-warning"
    >
      {icon}
      <div className="flex-1 text-status-info group-data-[variant=warn]:text-status-warning">
        {children}
      </div>
      {onDismiss && (
        <button
          type="button"
          onClick={onDismiss}
          className="shrink-0 cursor-pointer text-current opacity-40 transition-opacity hover:opacity-70"
          aria-label="Dismiss"
        >
          <X className="size-4" />
        </button>
      )}
    </div>
  );
}
