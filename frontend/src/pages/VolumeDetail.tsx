import { useParams } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Volume, ServiceRef, HistoryEntry } from "../api/types";
import InfoCard from "../components/InfoCard";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import ServiceRefList from "../components/ServiceRefList";
import { useSSE } from "../hooks/useSSE";
import { KeyValuePills, ResourceLink, Timestamp } from "../components/data";

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
    api
      .history({ resourceId: name, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [name]);

  useEffect(fetchData, [fetchData]);

  useSSE(["volume", "service"], (e) => {
    if (e.type === "volume" && e.id === name) fetchData();
    if (e.type === "service") fetchData();
  });

  if (error) return <FetchError message="Failed to load volume" />;
  if (!volume) return <LoadingDetail />;

  const labels = volume.Labels || {};
  const labelEntries = Object.entries(labels).filter(([k]) => k !== "com.docker.stack.namespace");
  const stack = labels["com.docker.stack.namespace"];
  const options = Object.entries(volume.Options || {});

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={volume.Name}
        breadcrumbs={[{ label: "Volumes", to: "/volumes" }, { label: volume.Name }]}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <InfoCard label="Driver" value={volume.Driver} />
        <InfoCard label="Scope" value={volume.Scope} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={volume.CreatedAt} />
        <InfoCard className="col-span-2" label="Mountpoint" value={volume.Mountpoint} />
      </div>

      {options.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Driver Options
          </h2>
          <KeyValuePills entries={options} />
        </div>
      )}

      {labelEntries.length > 0 && (
        <div>
          <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground mb-3">
            Labels
          </h2>
          <KeyValuePills entries={labelEntries} />
        </div>
      )}

      <ServiceRefList
        services={services}
        label="Mounted by Services"
        emptyMessage="No services using this volume."
      />

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
