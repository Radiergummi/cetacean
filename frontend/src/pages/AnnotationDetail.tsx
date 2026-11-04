import { api, ApiError } from "../api/client";
import {
  DeleteAnnotationDialog,
  EditAnnotationDialog,
} from "../components/annotations/AnnotationDialogs";
import type { EditedNote } from "../components/annotations/AnnotationForm";
import NoteText from "../components/annotations/NoteText";
import ShowOnChart from "../components/annotations/ShowOnChart";
import DetailError from "../components/DetailError";
import PageHeader from "../components/PageHeader";
import ResourceName from "../components/ResourceName";
import { Button, buttonVariants } from "../components/ui/button";
import { useDebouncedInvalidation } from "../hooks/useDebouncedInvalidation";
import { attribution } from "../lib/activity";
import { formatDateTime } from "../lib/format";
import { changesAnnotations } from "../lib/resourceEvents";
import { resourcePath } from "../lib/searchConstants";
import { useQuery } from "@tanstack/react-query";
import { Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";

/**
 * One annotation on its own page, which every list links its `@id` to: its
 * text and tags, its actions in the header, and the times, people and
 * resources in full.
 */
export default function AnnotationDetail() {
  const { id = "" } = useParams();
  const noteId = Number(id);

  const navigate = useNavigate();
  const [editing, setEditing] = useState<EditedNote | null>(null);
  const [deleting, setDeleting] = useState<EditedNote | null>(null);
  const { data, error, isLoading, refetch } = useQuery({
    queryKey: ["annotations", "detail", noteId],
    queryFn: ({ signal }) => api.annotation(noteId, signal),
    retry: (count, failure) =>
      !(failure instanceof ApiError && failure.status === 404) && count < 2,
  });

  useDebouncedInvalidation("/events", {
    timelineKeys: [["annotations", "detail", noteId]],
    delay: 1_000,
    relevant: changesAnnotations,
  });

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
            This annotation doesn't exist, or you may not read it.{" "}
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
            fallback="The annotation could not be read."
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
  const edited = { note, etag: data.etag };

  return (
    <div>
      <PageHeader
        title="Annotation"
        breadcrumbs={breadcrumbs}
        actions={
          <div className="flex items-center gap-2">
            <ShowOnChart
              note={note}
              className={buttonVariants({ variant: "outline", size: "sm" })}
            />

            {data.allowedMethods.has("PATCH") && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing(edited)}
              >
                <Pencil className="size-3.5" />
                Edit
              </Button>
            )}

            {data.allowedMethods.has("DELETE") && (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setDeleting(edited)}
              >
                <Trash2 className="size-3.5" />
                Delete
              </Button>
            )}
          </div>
        }
      />

      <EditAnnotationDialog
        editing={editing}
        onClose={() => setEditing(null)}
      />

      <DeleteAnnotationDialog
        deleting={deleting}
        onClose={() => setDeleting(null)}
        onDeleted={() => navigate("/annotations")}
      />

      <div
        data-cluster-wide={note.resources.length === 0 || undefined}
        className="mb-4 rounded-r-lg border border-l-2 border-l-annotation bg-card p-4 data-cluster-wide:border-l-annotation-cluster"
      >
        <p className="break-words whitespace-pre-wrap">
          <NoteText text={note.text} />
        </p>

        {note.tags.length > 0 && (
          <div className="mt-3 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
            {note.tags.map((tag) => (
              <Link
                key={tag}
                to={`/annotations?tag=${encodeURIComponent(tag)}`}
                className="rounded border px-1 hover:text-foreground"
              >
                #{tag}
              </Link>
            ))}
          </div>
        )}
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
