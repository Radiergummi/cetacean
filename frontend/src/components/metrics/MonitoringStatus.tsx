import type { MonitoringStatus as Status } from "../../api/types";
import { AlertTriangle, BarChart3, X } from "lucide-react";
import type React from "react";
import { useState } from "react";

const DISMISS_KEY = "cetacean:dismiss-monitoring-banner";

interface Props {
  status: Status;
  source?: "nodeExporter" | "cadvisor";
}

export default function MonitoringStatus({ status, source }: Props) {
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(DISMISS_KEY) === "true");

  // Fully healthy — nothing to show
  if (status.prometheusConfigured && status.prometheusReachable) {
    const neOk = !status.nodeExporter || status.nodeExporter.targets >= status.nodeExporter.nodes;
    const caOk = !status.cadvisor || status.cadvisor.targets >= status.cadvisor.nodes;
    if (neOk && caOk) return null;
  }

  // State A: Nothing configured (dismissible)
  if (!status.prometheusConfigured) {
    if (dismissed) return null;
    return (
      <Banner
        icon={<BarChart3 className="size-5 shrink-0 text-blue-600 dark:text-blue-400" />}
        border="border-blue-300 dark:border-blue-500/30"
        bg="bg-blue-50 dark:bg-blue-500/10"
        textColor="text-blue-800 dark:text-blue-200"
        onDismiss={() => {
          localStorage.setItem(DISMISS_KEY, "true");
          setDismissed(true);
        }}
      >
        <p className="text-sm">
          <strong>Monitoring not configured.</strong> Deploy the monitoring stack to enable CPU,
          memory, and disk metrics across your cluster.
        </p>
        <pre className="mt-2 max-w-fit overflow-x-auto rounded bg-blue-100 px-2 py-1 text-xs dark:bg-blue-500/10">
          docker stack deploy -c compose.monitoring.yaml cetacean-monitoring
        </pre>
        <p className="mt-3 text-xs">
          Then set{" "}
          <code className="rounded bg-blue-100 px-1 py-0.5 font-mono dark:bg-blue-500/20">
            CETACEAN_PROMETHEUS_URL
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
        icon={<AlertTriangle className="size-5 shrink-0 text-amber-600 dark:text-amber-400" />}
        border="border-amber-300 dark:border-amber-500/30"
        bg="bg-amber-50 dark:bg-amber-500/10"
        textColor="text-amber-800 dark:text-amber-200"
      >
        <p className="text-sm">
          <strong>Cannot reach Prometheus</strong> — metrics unavailable. Check that the Prometheus
          service is running and reachable from Cetacean.
        </p>
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

  if (hints.length === 0) return null;

  return (
    <Banner
      icon={<BarChart3 className="size-5 shrink-0 text-blue-600 dark:text-blue-400" />}
      border="border-blue-300 dark:border-blue-500/30"
      bg="bg-blue-50 dark:bg-blue-500/10"
      textColor="text-blue-800 dark:text-blue-200"
      onDismiss={() => {
        localStorage.setItem(DISMISS_KEY, "true");
        setDismissed(true);
      }}
    >
      <p className="text-sm">
        <strong>Monitoring partially configured</strong>
      </p>
      <ul className="mt-1 list-inside list-disc space-y-0.5 text-sm">
        {hints.map((h) => (
          <li key={h}>{h}</li>
        ))}
      </ul>
    </Banner>
  );
}

function Banner({
  icon,
  border,
  bg,
  textColor,
  onDismiss,
  children,
}: {
  icon: React.ReactNode;
  border: string;
  bg: string;
  textColor: string;
  onDismiss?: () => void;
  children: React.ReactNode;
}) {
  return (
    <div className={`flex items-start gap-3 rounded-lg border ${border} ${bg} mb-4 px-4 py-3`}>
      {icon}
      <div className={`flex-1 ${textColor}`}>{children}</div>
      {onDismiss && (
        <button
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
