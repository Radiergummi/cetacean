import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { SearchResponse } from "../api/types";
import EmptyState from "../components/EmptyState";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import { SearchInput } from "../components/search";
import { useSearchParam } from "../hooks/useSearchParam";
import { Loader2 } from "lucide-react";
import ResourceName from "../components/ResourceName";
import { resourcePath, statusColor, TYPE_LABELS, TYPE_ORDER } from "../lib/searchConstants";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return <Loader2 className="size-3 shrink-0 text-blue-500 animate-spin" />;
  }
  const color = statusColor(state);
  return <span className={`inline-block size-2 rounded-full shrink-0 ${color}`} title={state} />;
}

export default function SearchPage() {
  const [input, query, setInput] = useSearchParam("q");
  const [data, setData] = useState<SearchResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Fetch results when debounced q changes
  useEffect(() => {
    if (!query) {
      setData(null);
      setLoading(false);

      return;
    }

    let cancelled = false;
    setLoading(true);
    setError(null);

    api.search(query, 0).then(
      (response) => {
        if (!cancelled) {
          setData(response);
          setLoading(false);
        }
      },
      (error) => {
        if (!cancelled) {
          setError(error instanceof Error ? error.message : String(error));
          setLoading(false);
        }
      },
    );

    return () => {
      cancelled = true;
    };
  }, [query]);

  return (
    <>
      <PageHeader title="Search" />

      <div className="mb-6">
        <SearchInput value={input} onChange={setInput} placeholder="Search all resources…" />
      </div>

      {loading && <SkeletonTable columns={3} rows={8} />}

      {error && <p className="text-sm text-destructive">{error}</p>}

      {!loading && !error && query && data && data.total === 0 && (
        <EmptyState message={`No results for "${query}"`} />
      )}

      {!loading && !error && data && data.total > 0 && (
        <ul className="flex flex-col gap-8">
          {TYPE_ORDER.map((type) => {
            const items = data.results[type];

            if (!items || items.length === 0) {
              return null;
            }

            return (
              <li key={type} className="contents">
                <section>
                  <header className="mb-2">
                    <h2 className="text-sm font-medium text-muted-foreground">
                      <span>{TYPE_LABELS[type]}</span>
                      <span className="ml-2 inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
                        {items.length}
                      </span>
                    </h2>
                  </header>

                  <ul className="rounded-lg border divide-y">
                    {items.map((item) => (
                      <li key={item.id} className="contents">
                        <Link
                          to={resourcePath(type, item.id)}
                          className="flex items-center gap-3 px-4 py-2.5 hover:bg-muted/50 transition-colors"
                        >
                          {item.state && <StateOrb state={item.state} />}
                          <span className="font-medium text-sm truncate">
                            <ResourceName name={item.name} />
                          </span>

                          {item.detail && (
                            <span className="text-xs text-muted-foreground truncate">
                              {item.detail}
                            </span>
                          )}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </section>
              </li>
            );
          })}
        </ul>
      )}

      {!loading && !error && !query && (
        <p className="text-sm text-muted-foreground">Type to search across all resources.</p>
      )}
    </>
  );
}
