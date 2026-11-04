import NoteCard, { actionClass } from "@/components/annotations/NoteCard";
import { Popover, PopoverContent } from "@/components/ui/popover";
import type { TimelineItem } from "@/lib/timeline";
import { useEffect, useMemo, useState, useSyncExternalStore, type RefObject } from "react";
import { Link } from "react-router-dom";

/**
 * The notes a clicked flag or range groups, the time it was clicked at, and
 * the height to hang below, in the chart's pixels.
 */
export interface NotePin {
  time: number;
  y: number;
  ids: number[];
}

/**
 * The notes of a clicked flag, pinned below it with each card's menu and a
 * link to its page. They are read from the live timeline, so an edit shows
 * and a deleted note drops out. The pin follows its time across the chart:
 * `locate` places it, again whenever `subscribe` says the markers moved.
 */
export default function NotePopover({
  pin,
  container,
  timeline,
  locate,
  subscribe,
  onClose,
}: {
  pin: NotePin | null;
  container: RefObject<HTMLElement | null>;
  timeline: TimelineItem[];
  locate: (time: number) => number | null;
  subscribe: (onMove: () => void) => () => void;
  onClose: () => void;
}) {
  const notes = useMemo(
    () =>
      (pin?.ids ?? []).flatMap(
        (id) => timeline.find(({ kind, entry }) => kind === "annotation" && entry.id === id) ?? [],
      ),
    [pin, timeline],
  );
  const x = useSyncExternalStore(subscribe, () => (pin ? locate(pin.time) : null));

  // While a card's menu or dialog is open the pin holds what it showed, so
  // nothing unmounts the dialog; it unpins once the dialog closes if still gone.
  const [held, setHeld] = useState(false);
  const live = useMemo(() => ({ notes, x }), [notes, x]);
  const [onScreen, setOnScreen] = useState(live);

  if (!held && onScreen !== live) {
    setOnScreen(live);
  }

  const { notes: shownNotes, x: shownX } = held ? onScreen : live;
  const gone = pin !== null && (shownNotes.length === 0 || shownX === null);

  // Base UI reads an anchor once per identity, so a move makes a new one.
  const anchor = useMemo(
    () =>
      pin && shownX !== null
        ? {
            contextElement: container.current ?? undefined,
            getBoundingClientRect: () => {
              const bounds = container.current?.getBoundingClientRect();

              return new DOMRect((bounds?.left ?? 0) + shownX, (bounds?.top ?? 0) + pin.y);
            },
          }
        : null,
    [container, pin, shownX],
  );

  // Notes deleted, hidden or scrolled off the chart unpin, rather than come back later.
  useEffect(() => {
    if (gone) {
      onClose();
    }
  }, [gone, onClose]);

  return (
    <Popover
      open={!gone && pin !== null}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <PopoverContent
        aria-label={notes.length > 1 ? `${notes.length} annotations` : "Annotation"}
        side="bottom"
        align="start"
        sideOffset={10}
        anchor={anchor}
        className="max-h-96 w-96 max-w-[calc(100vw-2rem)] overflow-y-auto"
      >
        {shownNotes.map(({ entry }) => (
          <NoteCard
            key={entry.id}
            entry={entry}
            onActiveChange={setHeld}
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
