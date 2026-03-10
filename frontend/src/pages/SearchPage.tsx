import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { SearchResourceType, SearchResponse } from "../api/types";
import EmptyState from "../components/EmptyState";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import SearchInput from "../components/SearchInput";
import { useSearchParam } from "../hooks/useSearchParam";

const TYPE_ORDER: SearchResourceType[] = [
  "services", "stacks", "nodes", "tasks",
  "configs", "secrets", "networks", "volumes",
];

const TYPE_LABELS: Record<SearchResourceType, string> = {
  services: "Services", stacks: "Stacks", nodes: "Nodes", tasks: "Tasks",
  configs: "Configs", secrets: "Secrets", networks: "Networks", volumes: "Volumes",
};

function resourcePath(type: SearchResourceType, id: string): string {
  return `/${type}/${id}`;
}

export default function SearchPage() {
  const [input, q, setInput] = useSearchParam("q");
  const [data, setData] = useState<SearchResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Fetch results when debounced q changes
  useEffect(() => {
    if (!q) {
      setData(null);
      setLoading(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api.search(q, 0).then(
      (res) => {
        if (!cancelled) {
          setData(res);
          setLoading(false);
        }
      },
      (err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err));
          setLoading(false);
        }
      },
    );
    return () => { cancelled = true; };
  }, [q]);

  return (
    <>
      <PageHeader title="Search" />
      <div className="mb-6">
        <SearchInput
          value={input}
          onChange={setInput}
          placeholder="Search all resources..."
        />
      </div>
      {loading && <SkeletonTable columns={3} rows={8} />}
      {error && (
        <p className="text-sm text-destructive">{error}</p>
      )}
      {!loading && !error && q && data && data.total === 0 && (
        <EmptyState message={`No results for "${q}"`} />
      )}
      {!loading && !error && data && data.total > 0 && (
        <div className="space-y-8">
          {TYPE_ORDER.map((type) => {
            const items = data.results[type];
            if (!items || items.length === 0) return null;
            return (
              <section key={type}>
                <h2 className="text-sm font-medium text-muted-foreground mb-2">
                  {TYPE_LABELS[type]} ({items.length})
                </h2>
                <div className="rounded-lg border divide-y">
                  {items.map((item) => (
                    <Link
                      key={item.id}
                      to={resourcePath(type, item.id)}
                      className="flex items-baseline gap-3 px-4 py-2.5 hover:bg-muted/50 transition-colors"
                    >
                      <span className="font-medium text-sm truncate">{item.name}</span>
                      {item.detail && (
                        <span className="text-xs text-muted-foreground truncate">{item.detail}</span>
                      )}
                    </Link>
                  ))}
                </div>
              </section>
            );
          })}
        </div>
      )}
      {!loading && !error && !q && (
        <p className="text-sm text-muted-foreground">Type to search across all resources.</p>
      )}
    </>
  );
}
