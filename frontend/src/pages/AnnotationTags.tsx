import { api } from "../api/client";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useSearchParam } from "../hooks/useSearchParam";
import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { Link } from "react-router-dom";

/** The most tags `GET /annotations/tags` returns in one page. */
const tagLimit = 200;

/**
 * The tags on the notes the caller may read, most used first; each opens the
 * notes carrying it.
 */
export default function AnnotationTags() {
  const [search, debouncedSearch, setSearch] = useSearchParam("q");

  const { data, isLoading, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["annotations", "tags", debouncedSearch],
      queryFn: ({ pageParam, signal }) => api.annotationTagsPage(pageParam, signal),
      initialPageParam: api.annotationTagsPath(debouncedSearch, tagLimit),
      getNextPageParam: ({ next }) => next,
      placeholderData: keepPreviousData,
    });

  useDebouncedInvalidation("/events", {
    timelineKeys: [["annotations", "tags"]],
    delay: 2_000,
    relevant: ({ type }) => type === "annotation",
  });

  const tags = useMemo(() => data?.pages.flatMap(({ items }) => items) ?? [], [data]);

  return (
    <div>
      <PageHeader
        title="Annotation tags"
        breadcrumbs={[
          { label: "History", to: "/history" },
          { label: "Annotations", to: "/annotations" },
          { label: "Tags" },
        ]}
      />

      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Tags starting with…"
      />

      {error ? (
        <FetchError
          message={error.message}
          onRetry={() => refetch()}
        />
      ) : isLoading ? (
        <div className="h-24 rounded-lg border bg-card" />
      ) : tags.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">
          {debouncedSearch ? "No tag starts with that" : "No annotation carries a tag yet"}
        </p>
      ) : (
        <ul className="divide-y rounded-lg border bg-card">
          {tags.map(({ tag, count }) => (
            <li key={tag}>
              <Link
                to={`/annotations?tag=${encodeURIComponent(tag)}`}
                className="flex items-center justify-between px-4 py-2 text-sm hover:bg-muted/50"
              >
                <span className="font-medium">#{tag}</span>
                <span className="text-muted-foreground tabular-nums">
                  {count === 1 ? "1 annotation" : `${count} annotations`}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {hasNextPage && (
        <div className="mt-3 flex justify-center">
          <Button
            variant="outline"
            size="sm"
            disabled={isFetchingNextPage}
            onClick={() => fetchNextPage()}
          >
            {isFetchingNextPage ? "Loading…" : "Load more tags"}
          </Button>
        </div>
      )}
    </div>
  );
}
