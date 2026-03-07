import { useState } from "react";
import { useSwarmResource } from "../hooks/useSwarmResource";
import { useViewMode } from "../hooks/useViewMode";
import { api } from "../api/client";
import type { Network } from "../api/types";
import SearchInput from "../components/SearchInput";
import PageHeader from "../components/PageHeader";
import ViewToggle from "../components/ViewToggle";
import EmptyState from "../components/EmptyState";
import FetchError from "../components/FetchError";
import { LoadingPage } from "../components/LoadingSkeleton";

export default function NetworkList() {
  const {
    data: networks,
    loading,
    error,
    retry,
  } = useSwarmResource(api.networks, "network", (n: Network) => n.Id);
  const [search, setSearch] = useState("");
  const [viewMode, setViewMode] = useViewMode("networks");

  if (loading) return <LoadingPage />;
  if (error) return <FetchError message={error.message} onRetry={retry} />;

  const filtered = networks.filter((n) => n.Name.toLowerCase().includes(search.toLowerCase()));

  return (
    <div>
      <PageHeader title="Networks" />
      <div className="flex items-stretch gap-3 mb-4">
        <SearchInput value={search} onChange={setSearch} placeholder="Search networks..." />
        <ViewToggle mode={viewMode} onChange={setViewMode} />
      </div>
      {filtered.length === 0 ? (
        <EmptyState message={search ? "No networks match your search" : "No networks found"} />
      ) : viewMode === "table" ? (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full">
            <thead className="sticky top-0 z-10 bg-background">
              <tr className="border-b bg-muted/50">
                <th className="text-left p-3 text-sm font-medium">Name</th>
                <th className="text-left p-3 text-sm font-medium">Driver</th>
                <th className="text-left p-3 text-sm font-medium">Scope</th>
              </tr>
            </thead>
            <tbody>
              {filtered.map((net) => (
                <tr key={net.Id} className="border-b">
                  <td className="p-3 text-sm">{net.Name}</td>
                  <td className="p-3 text-sm">{net.Driver}</td>
                  <td className="p-3 text-sm">{net.Scope}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {filtered.map((net) => (
            <div key={net.Id} className="rounded-lg border bg-card p-4">
              <div className="font-medium mb-2 truncate">{net.Name}</div>
              <div className="flex items-center gap-3 text-xs text-muted-foreground">
                <span>{net.Driver}</span>
                <span>{net.Scope}</span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
