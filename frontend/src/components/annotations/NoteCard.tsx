import AnnotationMenu from "./AnnotationMenu";
import NoteText from "./NoteText";
import ShowOnChart from "./ShowOnChart";
import type { HistoryEntry } from "@/api/types";
import { isClusterWide } from "@/components/metrics/chartMarkers";
import ResourceName from "@/components/ResourceName";
import { attribution } from "@/lib/activity";
import { formatDateTime } from "@/lib/format";
import { resourcePath } from "@/lib/searchConstants";
import { describeAnnotationTime, seconds } from "@/lib/timeline";
import type { ReactNode } from "react";
import { Link } from "react-router-dom";

/**
 * The classes of the action at the end of a card's details.
 */
export const actionClass =
  "ms-auto inline-flex items-center gap-1 hover:text-foreground hover:underline";

/**
 * A note someone wrote, as a card: its text and menu, when it says it
 * happened, what it is about, its tags, and who wrote it. `action` ends the
 * details, "Show on chart" unless given.
 */
export default function NoteCard({
  entry,
  onActiveChange,
  action,
}: {
  entry: HistoryEntry;
  onActiveChange: (active: boolean) => void;
  action?: ReactNode | undefined;
}) {
  const { actor, editor, id, resources = [], tags = [], text, timestamp, until, updated } = entry;
  const clusterWide = isClusterWide({ entry });

  return (
    <div
      data-cluster-wide={clusterWide || undefined}
      className="min-w-0 flex-1 rounded-r-md border border-l-2 border-l-annotation px-2.5 py-1.5 text-sm data-cluster-wide:border-l-annotation-cluster"
    >
      <div className="flex items-start gap-2">
        <p className="min-w-0 flex-1 break-words whitespace-pre-wrap">
          <NoteText text={text ?? ""} />
        </p>
        <AnnotationMenu
          id={id}
          onActiveChange={onActiveChange}
        />
      </div>

      <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
        <Link
          to={`/annotations/${id}`}
          className="hover:text-foreground hover:underline"
        >
          {describeAnnotationTime(
            seconds(timestamp),
            until === undefined ? undefined : seconds(until),
          )}
        </Link>

        {clusterWide ? (
          <span>Cluster-wide</span>
        ) : (
          resources.map(({ type, id, name, removed }) => {
            const path = removed ? undefined : resourcePath(type, id, name);

            return path ? (
              <Link
                key={`${type}-${id}`}
                to={path}
                className="font-medium text-foreground hover:underline"
              >
                <ResourceName name={name} />
              </Link>
            ) : (
              <span
                key={`${type}-${id}`}
                title={removed ? "Removed from the cluster" : undefined}
                className={removed ? "line-through" : undefined}
              >
                <ResourceName name={name} />
              </span>
            );
          })
        )}

        {tags.map((tag, index) => (
          <Link
            key={`${index}-${tag}`}
            to={`/annotations?tag=${encodeURIComponent(tag)}`}
            className="rounded border px-1 hover:text-foreground"
          >
            #{tag}
          </Link>
        ))}

        {actor && <span>{attribution(actor)}</span>}

        {updated && (
          <span
            title={`Edited ${editor ? `${attribution(editor)} ` : ""}${formatDateTime(updated)}`}
          >
            edited
          </span>
        )}

        {action === undefined ? (
          <ShowOnChart
            note={entry}
            className={actionClass}
          />
        ) : (
          action
        )}
      </div>
    </div>
  );
}
