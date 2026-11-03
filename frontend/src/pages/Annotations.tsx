import { api } from "../api/client";
import type { AnnotationFilters } from "../api/types";
import ActivityFeed from "../components/ActivityFeed";
import AddAnnotationButton from "../components/annotations/AddAnnotationButton";
import { annotationEntry } from "../components/annotations/annotationFields";
import FetchError from "../components/FetchError";
import ListToolbar from "../components/ListToolbar";
import PageHeader from "../components/PageHeader";
import SegmentedControl from "../components/SegmentedControl";
import { Button, buttonVariants } from "../components/ui/button";
import { useAnnotationCapabilities } from "../hooks/useAnnotationCapabilities";
import { useAuth } from "../hooks/useAuth";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { useSearchParam } from "../hooks/useSearchParam";
import { apiPath } from "../lib/basePath";
import { keepPreviousData, useInfiniteQuery } from "@tanstack/react-query";
import { Download, Tags, X } from "lucide-react";
import { useMemo } from "react";
import { Link, useSearchParams } from "react-router-dom";

/** The most annotations `GET /annotations` returns in one page. */
const annotationLimit = 200;

const authorSegments = [
  { value: "anyone", label: "Anyone" },
  { value: "me", label: "Me" },
] as const;

/**
 * Every note the caller may read, filtered by tag, author and text. The
 * filters live in the URL, so a filtered list can be shared.
 */
export default function Annotations() {
  const [params, setParams] = useSearchParams();
  const [search, debouncedSearch, setSearch] = useSearchParam("q");
  const { identity } = useAuth();
  const { create } = useAnnotationCapabilities();

  const tags = params.getAll("tag");
  const authors = params.getAll("author");
  const matchAny = params.get("matchAny") === "true";
  const mine = identity !== null && authors.length === 1 && authors[0] === identity.subject;

  const filters: AnnotationFilters = {
    tags,
    matchAny,
    authors,
    ...(debouncedSearch ? { search: debouncedSearch } : {}),
  };

  const { data, isLoading, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: ["annotations", filters],
      queryFn: ({ pageParam, signal }) => api.annotationsPage(pageParam, signal),
      initialPageParam: api.annotationsPath({ ...filters, limit: annotationLimit }),
      getNextPageParam: ({ next }) => next,
      placeholderData: keepPreviousData,
    });

  useDebouncedInvalidation("/events", [["annotations"]], 2_000);

  const entries = useMemo(
    () => data?.pages.flatMap(({ items }) => items.map(annotationEntry)) ?? [],
    [data],
  );

  const update = (change: (next: URLSearchParams) => void) => {
    const next = new URLSearchParams(params);

    change(next);
    setParams(next, { replace: true });
  };

  const removeTag = (tag: string) =>
    update((next) => {
      next.delete("tag", tag);

      if (next.getAll("tag").length < 2) {
        next.delete("matchAny");
      }
    });

  const filtered = tags.length > 0 || authors.length > 0 || debouncedSearch !== "";
  const csvHref = apiPath(api.annotationsPath(filters).replace("/annotations", "/annotations.csv"));

  return (
    <div>
      <PageHeader
        title="Annotations"
        breadcrumbs={[{ label: "History", to: "/history" }, { label: "Annotations" }]}
        actions={
          <div className="flex items-center gap-2">
            {create && <AddAnnotationButton />}
            <Link
              to="/annotations/tags"
              className={buttonVariants({ variant: "outline", size: "sm" })}
            >
              <Tags className="size-3.5" />
              Tags
            </Link>
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
        {identity && (
          <SegmentedControl
            segments={[...authorSegments]}
            value={mine ? "me" : "anyone"}
            onChange={(value) =>
              update((next) => {
                next.delete("author");

                if (value === "me") {
                  next.set("author", identity.subject);
                }
              })
            }
          />
        )}

        {tags.map((tag) => (
          <span
            key={tag}
            className="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-sm"
          >
            #{tag}
            <button
              type="button"
              aria-label={`Remove the ${tag} filter`}
              onClick={() => removeTag(tag)}
              className="text-muted-foreground hover:text-foreground"
            >
              <X className="size-3.5" />
            </button>
          </span>
        ))}

        {tags.length > 1 && (
          <SegmentedControl
            segments={[
              { value: "all", label: "Every tag" },
              { value: "any", label: "Any tag" },
            ]}
            value={matchAny ? "any" : "all"}
            onChange={(value) =>
              update((next) => {
                next.delete("matchAny");

                if (value === "any") {
                  next.set("matchAny", "true");
                }
              })
            }
          />
        )}
      </div>

      <ListToolbar
        search={search}
        onSearchChange={setSearch}
        placeholder="Search the notes…"
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
            collapse={false}
            emptyText={filtered ? "No annotations match these filters" : "No annotations yet"}
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
            {isFetchingNextPage ? "Loading…" : "Load older annotations"}
          </Button>
        </div>
      )}
    </div>
  );
}
