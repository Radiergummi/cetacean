import { useState } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { api } from "../api/client";
import type { Secret } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { LoadingPage } from "../components/LoadingSkeleton";
import TimeAgo from "../components/TimeAgo";

export default function SecretList() {
  const {
    data: secrets,
    loading,
    error,
    retry,
  } = useSwarmResource(api.secrets, "secret", (s: Secret) => s.ID);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("secrets");

  if (loading) return <LoadingPage />;
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = secrets.filter((s) =>
    (s.Spec.Name || s.ID).toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div>
      <PageHeader title="Secrets" />
      <p className="text-sm text-muted-foreground mb-4">
        Metadata only. Secret values are never exposed.
      </p>
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search secrets..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No secrets match your search" : "No secrets found"} />
      ) : viewMode === "table" ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead className="sticky top-0 z-10 bg-background">
              <tr className="border-b bg-muted/50">
                <th className="text-left p-3 text-sm font-medium">Name</th>
                <th className="text-left p-3 text-sm font-medium">Created</th>
                <th className="text-left p-3 text-sm font-medium">Updated</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((secret) => (
                <tr key={secret.ID} className="border-b">
                  <td className="p-3 text-sm">{secret.Spec.Name || secret.ID}</td>
                  <td className="p-3 text-sm">
                    {secret.CreatedAt ? <TimeAgo date={secret.CreatedAt} /> : "\u2014"}
                  </td>
                  <td className="p-3 text-sm">
                    {secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt} /> : "\u2014"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((secret) => (
            <div key={secret.ID} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{secret.Spec.Name || secret.ID}</div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>
                  Created: {secret.CreatedAt ? <TimeAgo date={secret.CreatedAt} /> : "\u2014"}
                </div>
                <div>
                  Updated: {secret.UpdatedAt ? <TimeAgo date={secret.UpdatedAt} /> : "\u2014"}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
