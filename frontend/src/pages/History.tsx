import { api } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import { buttonVariants } from "../components/ui/button";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useSearchParam } from "../hooks/useSearchParam";
import { apiPath } from "../lib/basePath";
import { useQuery } from "@tanstack/react-query";
import { Download } from "lucide-react";
import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";

/** The most entries `GET /history` returns in one response. */
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
  const [params, setParams] = useSearchParams();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const requestedType = params.get("type");
  const type: TypeFilter = isTypeFilter(requestedType) ? requestedType : "all";
  const resourceType = type === "all" ? undefined : type;

  const {
    data: entries = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: ["history", { type: resourceType, limit: historyLimit }],
    queryFn: ({ signal }) =>
      api.history({ ...(resourceType ? { type: resourceType } : {}), limit: historyLimit }, signal),
  });

  useDebouncedInvalidation("/events", [["history"]], 2_000);

  const visible = useMemo(() => {
    const needle = debouncedSearch.toLowerCase();

    return needle === ""
      ? entries
      : entries.filter(({ name }) => name.toLowerCase().includes(needle));
  }, [entries, debouncedSearch]);

  function setType(next: TypeFilter) {
    setParams(
      (previous) => {
        const updated = new URLSearchParams(previous);

        if (next === "all") {
          updated.delete("type");
        } else {
          updated.set("type", next);
        }

        return updated;
      },
      { replace: true },
    );
  }

  const csvHref = apiPath(`/history.csv${resourceType ? `?type=${resourceType}` : ""}`);

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
            entries={visible}
            loading={isLoading}
            hideType={resourceType !== undefined}
          />
        </div>
      )}

      {entries.length >= historyLimit && (
        <p className="mt-3 text-xs text-muted-foreground">
          Showing the latest {historyLimit} events. The CSV download holds the full log.
        </p>
      )}
    </div>
  );
}
