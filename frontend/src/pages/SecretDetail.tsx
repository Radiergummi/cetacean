import { useParams } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Secret, ServiceRef, HistoryEntry } from "../api/types";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import ServiceRefList from "../components/ServiceRefList";
import { useSSE } from "../hooks/useSSE";
import { KeyValuePills, ResourceId, ResourceLink, Timestamp } from "../components/data";

export default function SecretDetail() {
  const { id } = useParams<{ id: string }>();
  const [secret, setSecret] = useState<Secret | null>(null);
  const [services, setServices] = useState<ServiceRef[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    api
      .secret(id)
      .then((d) => {
        setSecret(d.secret);
        setServices(d.services ?? []);
      })
      .catch(() => setError(true));
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useSSE(["secret", "service"], (e) => {
    if (e.type === "secret" && e.id === id) fetchData();
    if (e.type === "service") fetchData();
  });

  if (error) return <FetchError message="Failed to load secret" />;
  if (!secret) return <LoadingDetail />;

  const name = secret.Spec.Name || secret.ID;
  const labels = secret.Spec.Labels || {};
  const labelEntries = Object.entries(labels).filter(([k]) => k !== "com.docker.stack.namespace");
  const stack = labels["com.docker.stack.namespace"];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={name}
        breadcrumbs={[{ label: "Secrets", to: "/secrets" }, { label: name }]}
      />

      <p className="text-sm text-muted-foreground">
        Metadata only. Secret values are never exposed.
      </p>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <ResourceId label="ID" id={secret.ID} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={secret.CreatedAt} />
        <Timestamp label="Updated" date={secret.UpdatedAt} />
      </div>

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
        label="Used by Services"
        emptyMessage="No services using this secret."
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
