import { api } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import { Button, buttonVariants } from "../components/ui/button";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useSearchParam } from "../hooks/useSearchParam";
import { useUrlParam } from "../hooks/useUrlParam";
import { apiPath } from "../lib/basePath";
import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { useMemo } from "react";

/** The most entries `GET /history` returns in one page. */
const historyLimit = 200;

const typeSegments = [
  { value: "all", label: "All" },
  { value: "service", label: "Services" },
  { value: "task", label: "Tasks" },
  { value: "node", label: "Nodes" },
  { value: "stack", label: "Stacks" },
  { value: "config", label: "Configs" },
  { value: "secret", label: "Secrets" },
  { value: "network", label: "Networks" },
  { value: "volume", label: "Volumes" },
] as const;

type TypeFilter = (typeof typeSegments)[number]["value"];

function isTypeFilter(value: string | null): value is TypeFilter {
  return typeSegments.some((segment) => segment.value === value);
}

export default function History() {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const [requestedType, setType] = useUrlParam("type", "all");
  const type: TypeFilter = isTypeFilter(requestedType) ? requestedType : "all";
  const resourceType = type === "all" ? undefined : type;

  const filters = {
    ...(resourceType ? { type: resourceType } : {}),
    ...(debouncedSearch ? { search: debouncedSearch } : {}),
    limit: historyLimit,
  };

  // Filtered on the server, so a match on a page not yet loaded still shows.
  const { data, isLoading, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["history", filters],
      queryFn: ({ pageParam, signal }) => api.historyPage(pageParam, signal),
      initialPageParam: api.historyPath(filters),
      getNextPageParam: ({ next }) => next,
      placeholderData: keepPreviousData,
    });

  useDebouncedInvalidation("/events", [["history"]], 2_000);

  const entries = useMemo(() => data?.pages.flatMap(({ items }) => items) ?? [], [data]);

  const csvQuery = new URLSearchParams({
    ...(resourceType ? { type: resourceType } : {}),
    ...(debouncedSearch ? { search: debouncedSearch } : {}),
  }).toString();
  const csvHref = apiPath(`/history.csv${csvQuery ? `?${csvQuery}` : ""}`);

  return (
    <div>
      <PageHeader
        title="History"
        actions={
          <a
            href={csvHref}
            download
            className={buttonVariants({ variant: "outline", size: "sm" })}
          >
            <Download className="size-3.5" />
            Download CSV
          </a>
        }
      />

      <div className="mb-4">
        <SegmentedControl
          segments={[...typeSegments]}
          value={type}
          onChange={setType}
        />
      </div>

      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Filter by name…"
      />

      {error ? (
        <FetchError
          message={error.message}
          onRetry={() => refetch()}
        />
      ) : (
        <div className="rounded-lg border bg-card p-4">
          <ActivityFeed
            entries={entries}
            loading={isLoading}
            hideType={resourceType !== undefined}
          />
        </div>
      )}

      {hasNextPage && (
        <div className="mt-3 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            disabled={isFetchingNextPage}
            onClick={() => fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading…" : "Load older events"}
          </Button>
        </div>
      )}
    </div>
  );
}
