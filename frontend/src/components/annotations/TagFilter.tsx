import { parseTag } from "./annotationFields";
import { useTagFilterParams } from "./useTagFilterParams";
import { useTagSuggestions } from "./useTagSuggestions";
import SegmentedControl from "@/components/SegmentedControl";
import { Input } from "@/components/ui/input";
import { X } from "lucide-react";
import { useState } from "react";

/**
 * The URL's tag filter as chips: each removable, one more added from the tags
 * in use, and with two or more a choice between every tag and any of them.
 */
export default function TagFilter() {
  const { tags, matchAny, setTagFilter } = useTagFilterParams();
  const [typed, setTyped] = useState("");
  const { listId, datalist, picked } = useTagSuggestions(typed, tags);

  const [invalid, setInvalid] = useState(false);

  const add = (input: string) => {
    const tag = parseTag(input);

    // An invalid tag stays to be corrected; an empty one is nothing to add.
    if (tag === null) {
      setInvalid(input.trim() !== "");

      return;
    }

    if (!tags.includes(tag)) {
      setTagFilter([...tags, tag], matchAny);
    }

    setTyped("");
    setInvalid(false);
  };

  return (
    <div className="flex flex-wrap items-center gap-2">
      {tags.map((tag) => (
        <span
          key={tag}
          className="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-sm"
        >
          #{tag}
          <button
            type="button"
            aria-label={`Remove the ${tag} filter`}
            onClick={() =>
              setTagFilter(
                tags.filter((kept) => kept !== tag),
                matchAny,
              )
            }
            className="text-muted-foreground hover:text-foreground"
          >
            <X className="size-3.5" />
          </button>
        </span>
      ))}

      <Input
        aria-label="Filter by tag"
        aria-invalid={invalid || undefined}
        title={
          invalid
            ? "A tag takes lowercase letters, digits, dots, dashes and underscores."
            : undefined
        }
        placeholder="Filter by tag…"
        list={listId}
        autoComplete="off"
        value={typed}
        onChange={(event) => {
          const tag = picked(event);

          if (tag === null) {
            setTyped(event.target.value);
            setInvalid(false);
          } else {
            add(tag);
          }
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            add(typed);
          }
        }}
        className="w-36"
      />

      {datalist}

      {tags.length > 1 && (
        <SegmentedControl
          segments={[
            { value: "all", label: "Every tag" },
            { value: "any", label: "Any tag" },
          ]}
          value={matchAny ? "any" : "all"}
          onChange={(value) => setTagFilter(tags, value === "any")}
        />
      )}
    </div>
  );
}
