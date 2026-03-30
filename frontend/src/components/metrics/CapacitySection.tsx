import { api, type ClusterMetrics, type ClusterSnapshot } from "../../api/client";
import { formatBytes, formatNumber, formatPercentage } from "../../lib/format";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

function barColor(percent: number, isReservation: boolean): string {
  const high = isReservation ? 95 : 90;
  const mid = isReservation ? 80 : 70;

  if (percent >= high) {
    return "bg-red-500";
  }

  if (percent >= mid) {
    return "bg-amber-500";
  }

  return "bg-blue-500";
}

function Bar({
  label,
  percent,
  detail,
  isReservation,
  onClick,
}: {
  label: string;
  percent: number;
  detail: string;
  isReservation: boolean;
  onClick?: () => void;
}) {
  const clamped = Math.min(100, Math.max(0, percent));
  const Component = onClick ? "button" : "div";
  return (
    <Component
      type={onClick ? "button" : undefined}
      className={`rounded-lg border bg-card p-4 text-left${onClick ? " cursor-pointer transition-colors hover:border-foreground/20" : ""}`}
      onClick={onClick}
    >
      <div className="mb-2 flex justify-between text-xs text-muted-foreground">
        <span className="font-medium">
          {label}
          {isReservation ? " (reserved)" : ""}
        </span>
        <span className="tabular-nums">{formatPercentage(clamped, 0)}</span>
      </div>
      <div className="h-2 overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full transition-all ${barColor(clamped, isReservation)}`}
          style={{ width: `${clamped}%` }}
        />
      </div>
      <div className="mt-1.5 text-xs text-muted-foreground">{detail}</div>
    </Component>
  );
}

export default function CapacitySection({
  snapshot: { prometheusConfigured, reservedCPU, reservedMemory, totalCPU, totalMemory },
}: {
  snapshot: ClusterSnapshot;
}) {
  const navigate = useNavigate();
  const [metrics, setMetrics] = useState<ClusterMetrics | null>(null);
  const goToNodes = useCallback(() => navigate("/nodes"), [navigate]);

  useEffect(() => {
    if (!prometheusConfigured) {
      return;
    }

    let cancelled = false;

    const load = () => {
      api
        .clusterMetrics()
        .then((metrics) => {
          if (!cancelled) {
            setMetrics(metrics);
          }
        })
        .catch(console.warn);
    };
    load();
    const interval = setInterval(load, 30_000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [prometheusConfigured]);

  if (prometheusConfigured && !metrics) {
    return (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {[1, 2, 3].map((index) => (
          <div
            key={index}
            className="rounded-lg border bg-card p-4"
          >
            <div className="mb-2 h-3 w-16 rounded bg-muted" />
            <div className="h-2 rounded-full bg-muted" />
            <div className="mt-1.5 h-3 w-24 rounded bg-muted" />
          </div>
        ))}
      </div>
    );
  }

  if (prometheusConfigured && metrics) {
    return (
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <Bar
          label="CPU"
          percent={metrics.cpu.percent}
          detail={`${formatNumber(metrics.cpu.used, 1)} / ${formatNumber(metrics.cpu.total)} cores`}
          isReservation={false}
          onClick={goToNodes}
        />
        <Bar
          label="Memory"
          percent={metrics.memory.percent}
          detail={`${formatBytes(metrics.memory.used)} / ${formatBytes(metrics.memory.total)}`}
          isReservation={false}
          onClick={goToNodes}
        />
        <Bar
          label="Disk"
          percent={metrics.disk.percent}
          detail={`${formatBytes(metrics.disk.used)} / ${formatBytes(metrics.disk.total)}`}
          isReservation={false}
          onClick={goToNodes}
        />
      </div>
    );
  }

  const cpuReservedCores = reservedCPU / 1e9;
  const cpuPct = totalCPU > 0 ? (cpuReservedCores / totalCPU) * 100 : 0;
  const memPct = totalMemory > 0 ? (reservedMemory / totalMemory) * 100 : 0;

  return (
    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <Bar
        label="CPU"
        percent={cpuPct}
        detail={`${formatNumber(cpuReservedCores, 1)} / ${totalCPU} cores reserved`}
        isReservation={true}
        onClick={goToNodes}
      />
      <Bar
        label="Memory"
        percent={memPct}
        detail={`${formatBytes(reservedMemory)} / ${formatBytes(totalMemory)} reserved`}
        isReservation={true}
        onClick={goToNodes}
      />
    </div>
  );
}
