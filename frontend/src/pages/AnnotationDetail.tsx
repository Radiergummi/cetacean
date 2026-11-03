import { api, ApiError } from "../api/client";
import ActivityFeed from "../components/ActivityFeed";
import { annotationEntry } from "../components/annotations/annotationFields";
import DetailError from "../components/DetailError";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { attribution } from "../lib/activity";
import { formatDateTime } from "../lib/format";
import { resourcePath } from "../lib/searchConstants";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

/**
 * One note on its own page, which every list links its `@id` to: the card
 * with its actions, and the times, people and resources in full.
 */
export default function AnnotationDetail() {
  const { id = "" } = useParams();
  const noteId = Number(id);

  const { data, error, isLoading, refetch } = useQuery({
    queryKey: ["annotations", "detail", noteId],
    queryFn: ({ signal }) => api.annotation(noteId, signal),
    retry: (count, failure) =>
      !(failure instanceof ApiError && failure.status === 404) && count < 2,
  });

  useDebouncedInvalidation("/events", [["annotations", "detail", noteId]], 1_000);

  const breadcrumbs = [
    { label: "History", to: "/history" },
    { label: "Annotations", to: "/annotations" },
    { label: `#${id}` },
  ];

  if (error) {
    const gone = error instanceof ApiError && error.status === 404;

    return (
      <div>
        <PageHeader
          title="Annotation"
          breadcrumbs={breadcrumbs}
        />
        {gone ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            This note doesn't exist, or you may not read it.{" "}
            <Link
              to="/annotations"
              className="underline"
            >
              See every annotation
            </Link>
          </p>
        ) : (
          <DetailError
            error={error}
            fallback="The note could not be read."
            onRetry={() => refetch()}
          />
        )}
      </div>
    );
  }

  if (isLoading || !data) {
    return (
      <div>
        <PageHeader
          title="Annotation"
          breadcrumbs={breadcrumbs}
        />
        <div className="h-24 rounded-lg border bg-card" />
      </div>
    );
  }

  const note = data.data;

  return (
    <div>
      <PageHeader
        title="Annotation"
        breadcrumbs={breadcrumbs}
      />

      <div className="mb-4 rounded-lg border bg-card p-4">
        <ActivityFeed
          entries={[annotationEntry(note)]}
          collapse={false}
        />
      </div>

      <dl className="grid gap-x-6 gap-y-2 rounded-lg border bg-card p-4 text-sm sm:grid-cols-[max-content_1fr]">
        <dt className="text-muted-foreground">{note.until ? "From" : "At"}</dt>
        <dd>{formatDateTime(note.timestamp)}</dd>

        {note.until && (
          <>
            <dt className="text-muted-foreground">Until</dt>
            <dd>{formatDateTime(note.until)}</dd>
          </>
        )}

        <dt className="text-muted-foreground">About</dt>
        <dd className="flex flex-wrap gap-x-3 gap-y-1">
          {note.resources.length === 0
            ? "The whole cluster"
            : note.resources.map(({ type, id: resourceId, name, removed }) => {
                const path = removed ? undefined : resourcePath(type, resourceId, name);

                return (
                  <span key={`${type}-${resourceId}`}>
                    <span className="text-muted-foreground">{type} </span>
                    {path ? (
                      <Link
                        to={path}
                        className="font-medium hover:underline"
                      >
                        <ResourceName name={name} />
                      </Link>
                    ) : (
                      <span className="line-through">
                        <ResourceName name={name} />
                      </span>
                    )}
                    {removed && <span className="text-muted-foreground"> (removed)</span>}
                  </span>
                );
              })}
        </dd>

        {note.actor && (
          <>
            <dt className="text-muted-foreground">Written</dt>
            <dd>{attribution(note.actor)}</dd>
          </>
        )}

        {note.updated && (
          <>
            <dt className="text-muted-foreground">Edited</dt>
            <dd>
              {note.editor ? `${attribution(note.editor)}, ` : ""}
              {formatDateTime(note.updated)}
            </dd>
          </>
        )}
      </dl>
    </div>
  );
}
