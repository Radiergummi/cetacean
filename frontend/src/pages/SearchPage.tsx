import { api } from "../api/client";
import EmptyState from "../components/EmptyState";
import { SkeletonTable } from "../components/LoadingSkeleton";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import { SearchInput } from "../components/search";
import { useSearchParam } from "../hooks/useSearchParam";
import { resourcePath, statusColor, typeLabels, typeOrder } from "../lib/searchConstants";
import { getErrorMessage } from "../lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { Link } from "react-router-dom";

function StateOrb({ state }: { state: string }) {
  if (state === "updating") {
    return (
      <Loader2
        aria-label="Updating"
        className="size-3 shrink-0 animate-spin text-blue-500"
      />
    );
  }
  const color = statusColor(state);
  return (
    <span
      role="img"
      aria-label={state}
      className={`inline-block size-2 shrink-0 rounded-full ${color}`}
    />
  );
}

export default function SearchPage() {
  const [input, query, setInput] = useSearchParam("q");

  const {
    data,
    isLoading: loading,
    error: queryError,
  } = useQuery({
    queryKey: ["search", query],
    queryFn: ({ signal }) => api.search(query!, 0, signal),
    enabled: !!query,
  });

  const error = queryError ? getErrorMessage(queryError, String(queryError)) : null;

  return (
    <>
      <PageHeader title="Search" />

      <div className="mb-6">
        <SearchInput
          value={input}
          onChange={setInput}
          placeholder="Search all resources…"
        />
      </div>

      {loading && (
        <SkeletonTable
          columns={3}
          rows={8}
        />
      )}

      {error && <p className="text-sm text-destructive">{error}</p>}

      {!loading && !error && query && data && data.total === 0 && (
        <EmptyState message={`No results for "${query}"`} />
      )}

      {!loading && !error && data && data.total > 0 && (
        <ul className="flex flex-col gap-8">
          {typeOrder.map((type) => {
            const items = data.results[type];

            if (!items || items.length === 0) {
              return null;
            }

            return (
              <li
                key={type}
                className="contents"
              >
                <section>
                  <header className="mb-2">
                    <h2 className="text-sm font-medium text-muted-foreground">
                      <span>{typeLabels[type]}</span>
                      <span className="ms-2 inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-xs font-medium">
                        {items.length}
                      </span>
                    </h2>
                  </header>

                  <ul className="divide-y rounded-lg border">
                    {items.map((item) => (
                      <li
                        key={item.id}
                        className="contents"
                      >
                        <Link
                          to={resourcePath(type, item.id)!}
                          className="flex items-center gap-3 px-4 py-2.5 transition-colors hover:bg-muted/50"
                        >
                          {item.state && <StateOrb state={item.state} />}
                          <span className="truncate text-sm font-medium">
                            <ResourceName name={item.name} />
                          </span>

                          {item.detail && (
                            <span className="truncate text-xs text-muted-foreground">
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
