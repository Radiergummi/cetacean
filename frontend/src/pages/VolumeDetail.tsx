import { useParams, Link } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Volume, ServiceRef, HistoryEntry } from "../api/types";
import InfoCard from "../components/InfoCard";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import { useSSE } from "../hooks/useSSE";
import { ResourceLink, Timestamp } from "../components/data";

export default function VolumeDetail() {
  const { name } = useParams<{ name: string }>();
  const [volume, setVolume] = useState<Volume | null>(null);
  const [services, setServices] = useState<ServiceRef[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!name) return;
    api
      .volume(name)
      .then((d) => {
        setVolume(d.volume);
        setServices(d.services ?? []);
      })
      .catch(() => setError(true));
    api.history({ resourceId: name, limit: 10 }).then(setHistory).catch(() => {});
  }, [name]);

  useEffect(fetchData, [fetchData]);

  useSSE(["volume", "service"], (e) => {
    if (e.type === "volume" && e.id === name) fetchData();
    if (e.type === "service") fetchData();
  });

  if (error) return <FetchError message="Failed to load volume" />;
  if (!volume) return <LoadingDetail />;

  const labels = volume.Labels || {};
  const labelEntries = Object.entries(labels).filter(
    ([k]) => k !== "com.docker.stack.namespace",
  );
  const stack = labels["com.docker.stack.namespace"];
  const options = Object.entries(volume.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={volume.Name}
        breadcrumbs={[{ label: "Volumes", to: "/volumes" }, { label: volume.Name }]}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <InfoCard label="Driver" value={volume.Driver} />
        <InfoCard label="Scope" value={volume.Scope} />
        <InfoCard label="Mountpoint" value={volume.Mountpoint} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={volume.CreatedAt} />
      </div>

      {options.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Driver Options
          </h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Key</th>
                  <th className="text-left p-3 text-sm font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {options.map(([k, v]) => (
                  <tr key={k} className="border-b last:border-b-0">
                    <td className="p-3 text-sm font-mono">{k}</td>
                    <td className="p-3 text-sm font-mono">{v}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {labelEntries.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Labels
          </h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Key</th>
                  <th className="text-left p-3 text-sm font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {labelEntries.map(([k, v]) => (
                  <tr key={k} className="border-b last:border-b-0">
                    <td className="p-3 text-sm font-mono">{k}</td>
                    <td className="p-3 text-sm font-mono">{v}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {services.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Mounted by Services
          </h2>
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 text-sm font-medium">Service</th>
                </tr>
              </thead>
              <tbody>
                {services.map((svc) => (
                  <tr key={svc.id} className="border-b last:border-b-0">
                    <td className="p-3 text-sm">
                      <Link to={`/services/${svc.id}`} className="text-link hover:underline">
                        {svc.name || svc.id.slice(0, 12)}
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {history.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Recent Activity
          </h2>
          <ActivityFeed entries={history} />
        </div>
      )}
    </div>
  );
}
