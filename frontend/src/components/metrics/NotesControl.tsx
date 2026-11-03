import type { useNoteFilter } from "./useNoteFilter";
import { MenuCheckboxItem, MenuContent } from "@/components/ui/menu";
import type { TimelineItem } from "@/lib/timeline";
import { Menu } from "@base-ui/react/menu";
import { Check, StickyNote } from "lucide-react";

/**
 * A panel's notes menu: show or hide the notes on its charts, and narrow them
 * to the tags of the notes in view, plus any narrowed to already.
 */
export default function NotesControl({
  timeline,
  filter: { shown, tags, setShown, toggleTag },
}: {
  timeline: TimelineItem[];
  filter: ReturnType<typeof useNoteFilter>;
}) {
  const inView = timeline.flatMap(({ kind, entry }) =>
    kind === "annotation" ? (entry.tags ?? []) : [],
  );
  const offered = [...new Set([...inView, ...tags])].sort();
  const narrowed = !shown || tags.length > 0;

  return (
    <Menu.Root modal={false}>
      <Menu.Trigger
        title="Notes"
        aria-label={narrowed ? "Notes, filtered" : "Notes"}
        data-narrowed={narrowed || undefined}
        className="relative flex size-8 cursor-pointer items-center justify-center rounded-md border border-border bg-background outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <StickyNote className="size-3.5" />
        {narrowed && (
          <span className="absolute top-1 right-1 size-1.5 rounded-full bg-annotation" />
        )}
      </Menu.Trigger>

      <MenuContent className="max-h-80 overflow-y-auto">
        <MenuCheckboxItem
          checked={shown}
          onCheckedChange={setShown}
          closeOnClick={false}
        >
          Show notes
          <Menu.CheckboxItemIndicator>
            <Check className="size-3.5" />
          </Menu.CheckboxItemIndicator>
        </MenuCheckboxItem>

        {offered.length > 0 && (
          <Menu.Group>
            <Menu.GroupLabel className="px-2 pt-2 pb-1 text-xs text-muted-foreground">
              Only notes tagged
            </Menu.GroupLabel>

            {offered.map((tag) => (
              <MenuCheckboxItem
                key={tag}
                checked={tags.includes(tag)}
                onCheckedChange={() => toggleTag(tag)}
                disabled={!shown}
                closeOnClick={false}
              >
                #{tag}
                <Menu.CheckboxItemIndicator>
                  <Check className="size-3.5" />
                </Menu.CheckboxItemIndicator>
              </MenuCheckboxItem>
            ))}
          </Menu.Group>
        )}
      </MenuContent>
    </Menu.Root>
  );
}
