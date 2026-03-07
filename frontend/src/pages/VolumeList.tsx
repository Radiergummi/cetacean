import { useState } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { api } from "../api/client";
import type { Volume } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { LoadingPage } from "../components/LoadingSkeleton";

export default function VolumeList() {
  const {
    data: volumes,
    loading,
    error,
    retry,
  } = useSwarmResource(api.volumes, "volume", (v: Volume) => v.Name);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("volumes");

  if (loading) return <LoadingPage />;
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = volumes.filter((v) => v.Name.toLowerCase().includes(search.toLowerCase()));

  return (
    <div>
      <PageHeader title="Volumes" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search volumes..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No volumes match your search" : "No volumes found"} />
      ) : viewMode === "table" ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="text-left p-3 text-sm font-medium">Name</th>
                <th className="text-left p-3 text-sm font-medium">Driver</th>
                <th className="text-left p-3 text-sm font-medium">Scope</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((vol) => (
                <tr key={vol.Name} className="border-b">
                  <td className="p-3 text-sm">{vol.Name}</td>
                  <td className="p-3 text-sm">{vol.Driver}</td>
                  <td className="p-3 text-sm">{vol.Scope}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((vol) => (
            <div key={vol.Name} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{vol.Name}</div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{vol.Driver}</span>
                <span>{vol.Scope}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
