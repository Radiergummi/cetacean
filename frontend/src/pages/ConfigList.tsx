import { useState } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { api } from "../api/client";
import type { Config } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { LoadingPage } from "../components/LoadingSkeleton";
import TimeAgo from "../components/TimeAgo";

export default function ConfigList() {
  const {
    data: configs,
    loading,
    error,
    retry,
  } = useSwarmResource(api.configs, "config", (c: Config) => c.ID);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("configs");

  if (loading) return <LoadingPage />;
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = configs.filter((c) =>
    (c.Spec.Name || c.ID).toLowerCase().includes(search.toLowerCase()),
  );

  return (
    <div>
      <PageHeader title="Configs" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search configs..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No configs match your search" : "No configs found"} />
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
              {filtered.map((cfg) => (
                <tr key={cfg.ID} className="border-b">
                  <td className="p-3 text-sm">{cfg.Spec.Name || cfg.ID}</td>
                  <td className="p-3 text-sm">
                    {cfg.CreatedAt ? <TimeAgo date={cfg.CreatedAt} /> : "\u2014"}
                  </td>
                  <td className="p-3 text-sm">
                    {cfg.UpdatedAt ? <TimeAgo date={cfg.UpdatedAt} /> : "\u2014"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((cfg) => (
            <div key={cfg.ID} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{cfg.Spec.Name || cfg.ID}</div>
              <div className="space-y-1 text-xs text-muted-foreground">
                <div>Created: {cfg.CreatedAt ? <TimeAgo date={cfg.CreatedAt} /> : "\u2014"}</div>
                <div>Updated: {cfg.UpdatedAt ? <TimeAgo date={cfg.UpdatedAt} /> : "\u2014"}</div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
