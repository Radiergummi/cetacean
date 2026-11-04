import { api } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import AddAnnotationButton from "../components/annotations/AddAnnotationButton";
import TagSearch from "../components/annotations/TagSearch";
import { useTagFilterParams } from "../components/annotations/useTagFilterParams";
import FetchError from "../components/FetchError";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import { Button, buttonVariants } from "../components/ui/button";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
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

const kindSegments = [
  { value: "all", label: "All" },
  { value: "change", label: "Changes" },
  { value: "annotation", label: "Annotations" },
] as const;

type KindFilter = (typeof kindSegments)[number]["value"];

function isKindFilter(value: string | null): value is KindFilter {
  return kindSegments.some((segment) => segment.value === value);
}

export default function History() {
  const [search] = useUrlParam("q", "");
  const [requestedType, setType] = useUrlParam("type", "all");
  const [requestedKind, setKind] = useUrlParam("kind", "all");
  const type: TypeFilter = isTypeFilter(requestedType) ? requestedType : "all";
  const resourceType = type === "all" ? undefined : type;
  const kind: KindFilter = isKindFilter(requestedKind) ? requestedKind : "all";
  const { tags, matchAny } = useTagFilterParams();

  const filters = {
    ...(resourceType ? { type: resourceType } : {}),
    ...(kind === "all" ? {} : { kind }),
    ...(search ? { search } : {}),
    tags,
    matchAny,
  };
  const filtered = type !== "all" || kind !== "all" || search !== "" || tags.length > 0;

  // Filtered on the server, so a match on a page not yet loaded still shows.
  const { data, isLoading, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["history", filters],
      queryFn: ({ pageParam, signal }) => api.historyPage(pageParam, signal),
      initialPageParam: api.historyPath({ ...filters, limit: historyLimit }),
      getNextPageParam: ({ next }) => next,
      placeholderData: keepPreviousData,
    });

  useDebouncedInvalidation("/events", { timelineKeys: [["history"]], delay: 2_000 });

  const entries = useMemo(() => data?.pages.flatMap(({ items }) => items) ?? [], [data]);
  const create = data?.pages[0]?.annotatable ?? false;

  const csvHref = apiPath(api.historyPath(filters).replace("/history", "/history.csv"));

  return (
    <div>
      <PageHeader
        title="History"
        actions={
          <div className="flex items-center gap-2">
            {create && <AddAnnotationButton />}
            <a
              href={csvHref}
              download
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              <Download className="size-3.5" />
              Download CSV
            </a>
          </div>
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <SegmentedControl
          segments={[...typeSegments]}
          value={type}
          onChange={setType}
        />

        <SegmentedControl
          segments={[...kindSegments]}
          value={kind}
          onChange={setKind}
        />
      </div>

      <TagSearch placeholder="Search everything shown, or type # for a tag…" />

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
            collapse={false}
            emptyText={
              kind === "change" && tags.length > 0
                ? "Changes carry no tags; remove the tag filter to see them"
                : hasNextPage
                  ? "No changes among the newest events"
                  : filtered
                    ? "Nothing matches these filters"
                    : undefined
            }
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
