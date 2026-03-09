import { useParams, Link } from "react-router-dom";
import { useState, useEffect, useCallback } from "react";
import { api } from "../api/client";
import type { Secret, ServiceRef, HistoryEntry } from "../api/types";
import InfoCard from "../components/InfoCard";
import PageHeader from "../components/PageHeader";
import { LoadingDetail } from "../components/LoadingSkeleton";
import FetchError from "../components/FetchError";
import ActivityFeed from "../components/ActivityFeed";
import TimeAgo from "../components/TimeAgo";
import { useSSE } from "../hooks/useSSE";

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
    api.history({ resourceId: id, limit: 10 }).then(setHistory).catch(() => {});
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
  const labelEntries = Object.entries(labels).filter(
    ([k]) => k !== "com.docker.stack.namespace",
  );
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

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <InfoCard label="ID" value={secret.ID} />
        {stack && <InfoCard label="Stack" value={stack} href={`/stacks/${stack}`} />}
        <InfoCard
          label="Created"
          value={secret.CreatedAt ? <TimeAgo date={secret.CreatedAt} /> : undefined}
        />
        <InfoCard
          label="Updated"
          value={secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt} /> : undefined}
        />
      </div>

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
            Used by Services
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
