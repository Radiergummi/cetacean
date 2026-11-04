import NoteCard, { actionClass } from "@/components/annotations/NoteCard";
import { Popover, PopoverContent } from "@/components/ui/popover";
import type { TimelineItem } from "@/lib/timeline";
import { useEffect, type RefObject } from "react";
import { Link } from "react-router-dom";

/**
 * Where a flag was clicked, in the chart's pixels, and the notes it groups.
 */
export interface NotePin {
  x: number;
  y: number;
  ids: number[];
}

const ignoreActive = () => {};

/**
 * The notes of a clicked flag, pinned below it with each card's menu and a
 * link to its page. They are read from the live timeline, so an edit shows
 * and a deleted note drops out.
 */
export default function NotePopover({
  pin,
  container,
  timeline,
  onClose,
}: {
  pin: NotePin | null;
  container: RefObject<HTMLElement | null>;
  timeline: TimelineItem[];
  onClose: () => void;
}) {
  const notes = (pin?.ids ?? []).flatMap(
    (id) => timeline.find(({ kind, entry }) => kind === "annotation" && entry.id === id) ?? [],
  );
  const gone = pin !== null && notes.length === 0;

  // Notes deleted, hidden or out of the window unpin, rather than come back later.
  useEffect(() => {
    if (gone) {
      onClose();
    }
  }, [gone, onClose]);

  return (
    <Popover
      open={pin !== null && notes.length > 0}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <PopoverContent
        aria-label={notes.length > 1 ? `${notes.length} notes` : "Note"}
        side="bottom"
        align="start"
        sideOffset={10}
        anchor={() => {
          const bounds = container.current?.getBoundingClientRect();

          return bounds && pin
            ? { getBoundingClientRect: () => new DOMRect(bounds.left + pin.x, bounds.top + pin.y) }
            : null;
        }}
        className="max-h-96 w-96 max-w-[calc(100vw-2rem)] overflow-y-auto"
      >
        {notes.map(({ entry }) => (
          <NoteCard
            key={entry.id}
            entry={entry}
            onActiveChange={ignoreActive}
            action={
              <Link
                to={`/annotations/${entry.id}`}
                className={actionClass}
              >
                Open
              </Link>
            }
          />
        ))}
      </PopoverContent>
    </Popover>
  );
}
