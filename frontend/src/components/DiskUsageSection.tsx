import { useEffect, useState } from "react";
import { api } from "../api/client";
import type { DiskUsageSummary } from "../api/types";
import { formatBytes } from "../lib/formatBytes";
import SectionHeader from "./data/SectionHeader";

const typeLabels: Record<string, string> = {
  images: "Images",
  containers: "Containers",
  volumes: "Volumes",
  buildCache: "Build Cache",
};

function reclaimableCell(reclaimable: number, total: number) {
  if (reclaimable <= 0) return "0 B";
  const pct = total > 0 ? Math.round((reclaimable / total) * 100) : 0;
  return <>{formatBytes(reclaimable)} <span className="ml-1">({pct}%)</span></>;
}

function DiskUsageTable({ data }: { data: DiskUsageSummary[] }) {
  const total = data.reduce((sum, d) => sum + d.totalSize, 0);
  const reclaimable = data.reduce((sum, d) => sum + d.reclaimable, 0);

  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="text-left p-3 font-medium">Type</th>
            <th className="text-right p-3 font-medium">Count</th>
            <th className="text-right p-3 font-medium">Active</th>
            <th className="text-right p-3 font-medium">Size</th>
            <th className="text-right p-3 font-medium">Reclaimable</th>
          </tr>
        </thead>
        <tbody>
          {data.map((d) => (
            <tr key={d.type} className="border-b last:border-b-0">
              <td className="p-3">{typeLabels[d.type] || d.type}</td>
              <td className="p-3 text-right tabular-nums">{d.count}</td>
              <td className="p-3 text-right tabular-nums">{d.active}</td>
              <td className="p-3 text-right tabular-nums">{d.totalSize > 0 ? formatBytes(d.totalSize) : "0 B"}</td>
              <td className="p-3 text-right tabular-nums text-muted-foreground">
                {reclaimableCell(d.reclaimable, d.totalSize)}
              </td>
            </tr>
          ))}
        </tbody>
        {total > 0 && (
          <tfoot>
            <tr className="border-t bg-muted/30">
              <td className="p-3 font-medium">Total</td>
              <td className="p-3" />
              <td className="p-3" />
              <td className="p-3 text-right tabular-nums font-medium">{formatBytes(total)}</td>
              <td className="p-3 text-right tabular-nums text-muted-foreground font-medium">
                {reclaimableCell(reclaimable, total)}
              </td>
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}

function DiskUsageLoading() {
  return (
    <div className="rounded-lg border bg-card overflow-hidden">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b bg-muted/50">
            <th className="text-left p-3 font-medium">Type</th>
            <th className="text-right p-3 font-medium">Count</th>
            <th className="text-right p-3 font-medium">Active</th>
            <th className="text-right p-3 font-medium">Size</th>
            <th className="text-right p-3 font-medium">Reclaimable</th>
          </tr>
        </thead>
        <tbody>
          {[1, 2, 3, 4].map((i) => (
            <tr key={i} className="border-b last:border-b-0">
              <td className="p-3"><div className="h-4 w-20 bg-muted rounded animate-pulse" /></td>
              <td className="p-3"><div className="h-4 w-8 bg-muted rounded animate-pulse ml-auto" /></td>
              <td className="p-3"><div className="h-4 w-8 bg-muted rounded animate-pulse ml-auto" /></td>
              <td className="p-3"><div className="h-4 w-16 bg-muted rounded animate-pulse ml-auto" /></td>
              <td className="p-3"><div className="h-4 w-24 bg-muted rounded animate-pulse ml-auto" /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/**
 * When `nodeId` is provided, only renders if the node matches the Docker host
 * Cetacean is connected to (disk usage data is local to that host).
 */
export default function DiskUsageSection({ nodeId }: { nodeId?: string }) {
  const [data, setData] = useState<DiskUsageSummary[] | null>(null);
  const [visible, setVisible] = useState(!nodeId);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!nodeId) return;
    api.cluster().then((snap) => {
      if (snap.localNodeID && snap.localNodeID === nodeId) {
        setVisible(true);
      } else {
        setLoading(false);
      }
    }).catch(() => setLoading(false));
  }, [nodeId]);

  useEffect(() => {
    if (!visible) return;
    api
      .diskUsage()
      .then(setData)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [visible]);

  if (!visible || (!loading && !data)) {
    return null;
  }

  return (
    <div>
      <SectionHeader title="Docker Disk Usage" />
      {data ? <DiskUsageTable data={data} /> : <DiskUsageLoading />}
    </div>
  );
}
