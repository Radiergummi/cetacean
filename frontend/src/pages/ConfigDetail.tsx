import { useParams } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Config, ServiceRef, HistoryEntry } from "../api/types";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import ServiceRefList from "../components/ServiceRefList";
import { useSSE } from "../hooks/useSSE";
import { KeyValuePills, ResourceId, ResourceLink, SectionHeader, Timestamp } from "../components/data";
import CodeBlock from "../components/CodeBlock";

export default function ConfigDetail() {
  const { id } = useParams<{ id: string }>();
  const [config, setConfig] = useState<Config | null>(null);
  const [services, setServices] = useState<ServiceRef[]>([]);
  const [history, setHistory] = useState<HistoryEntry[]>([]);
  const [error, setError] = useState(false);

  const fetchData = useCallback(() => {
    if (!id) return;
    api
      .config(id)
      .then((d) => {
        setConfig(d.config);
        setServices(d.services ?? []);
      })
      .catch(() => setError(true));
    api
      .history({ resourceId: id, limit: 10 })
      .then(setHistory)
      .catch(() => {});
  }, [id]);

  useEffect(fetchData, [fetchData]);

  useSSE(["config", "service"], (e) => {
    if (e.type === "config" && e.id === id) fetchData();
    if (e.type === "service") fetchData();
  });

  if (error) return <FetchError message="Failed to load config" />;
  if (!config) return <LoadingDetail />;

  const name = config.Spec.Name || config.ID;
  const labels = config.Spec.Labels || {};
  const labelEntries = Object.entries(labels).filter(([k]) => k !== "com.docker.stack.namespace");
  const stack = labels["com.docker.stack.namespace"];
  const data = config.Spec.Data;
  let decoded: string | null = null;
  if (data) {
    try {
      decoded = atob(data);
    } catch {
      decoded = null;
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={name}
        breadcrumbs={[{ label: "Configs", to: "/configs" }, { label: name }]}
      />

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        <ResourceId label="ID" id={config.ID} />
        <ResourceLink label="Stack" name={stack} to={`/stacks/${stack}`} />
        <Timestamp label="Created" date={config.CreatedAt} />
        <Timestamp label="Updated" date={config.UpdatedAt} />
      </div>

      {labelEntries.length > 0 && (
        <div>
          <SectionHeader title="Labels" />
          <KeyValuePills entries={labelEntries} />
        </div>
      )}

      {decoded != null && (
        <div>
          <SectionHeader title="Data" />
          <CodeBlock code={decoded} />
        </div>
      )}

      <ServiceRefList
        services={services}
        label="Used by Services"
        emptyMessage="No services using this config."
      />

      {history.length > 0 && (
        <div>
          <SectionHeader title="Recent Activity" />
          <ActivityFeed entries={history} />
        </div>
      )}
    </div>
  );
}
