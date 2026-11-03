import { parseTag } from "./annotationFields";
import { useTagFilterParams } from "./useTagFilterParams";
import { useTagSuggestions } from "./useTagSuggestions";
import SegmentedControl from "@/components/SegmentedControl";
import { useSearchParam } from "@/hooks/useSearchParam";
import { useUrlParam } from "@/hooks/useUrlParam";
import { Search, X } from "lucide-react";
import { useId, useState, type KeyboardEvent } from "react";

/**
 * The tag being typed: a word starting with `#` at the end of the input.
 */
const tagToken = /(?:^|\s)#(\S*)$/;

/**
 * The input without the tag being typed, which is the free-text search.
 */
function freeText(input: string): string {
  return input.replace(tagToken, "").trim();
}

/**
 * One search box for a list of annotations or history: words search the text,
 * and a word starting with `#` is a tag, completed from the tags in use and
 * kept as a chip. The text is the URL's `q`, the chips its `tag`s.
 */
export default function TagSearch({ placeholder }: { placeholder: string }) {
  const { tags, matchAny, setTagFilter } = useTagFilterParams();
  const [urlText] = useUrlParam("q", "");
  const [, , setSearch] = useSearchParam("q");
  const [input, setInput] = useState(urlText);
  const [seenText, setSeenText] = useState(urlText);
  const [highlight, setHighlight] = useState(0);
  const [dismissed, setDismissed] = useState(false);
  const [invalid, setInvalid] = useState(false);
  const listboxId = useId();
  const hintId = useId();

  // A search changed elsewhere, as by Back, replaces what is typed.
  if (urlText !== seenText) {
    setSeenText(urlText);

    if (freeText(input) !== urlText) {
      setInput(urlText);
    }
  }

  const token = tagToken.exec(input)?.[1];
  const { suggestions } = useTagSuggestions(token ?? "", tags);
  // The fetched list trails the typing, so it is narrowed to what is typed now.
  const options =
    token === undefined || dismissed
      ? []
      : suggestions.filter((tag) => tag.includes(token.toLowerCase()));
  const open = options.length > 0;
  const optionId = (index: number) => `${listboxId}-${index}`;

  const change = (value: string) => {
    setInput(value);
    setHighlight(0);
    setDismissed(false);
    setInvalid(false);
    setSearch(freeText(value));
  };

  const commit = (typed: string) => {
    const tag = parseTag(typed);

    if (tag === null) {
      setInvalid(true);

      return;
    }

    if (!tags.includes(tag)) {
      setTagFilter([...tags, tag], matchAny);
    }

    setInput(input.replace(/#\S*$/, ""));
    setHighlight(0);
    setInvalid(false);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === "Backspace" && input === "" && tags.length > 0) {
      setTagFilter(tags.slice(0, -1), matchAny);

      return;
    }

    if (token === undefined) {
      return;
    }

    if (open && (event.key === "ArrowDown" || event.key === "ArrowUp")) {
      event.preventDefault();
      setHighlight((current) =>
        event.key === "ArrowDown"
          ? Math.min(current + 1, options.length - 1)
          : Math.max(current - 1, 0),
      );

      return;
    }

    if (event.key === "Escape" && open) {
      event.preventDefault();
      setDismissed(true);

      return;
    }

    // Tab and Enter complete to the highlighted tag; Space keeps what is typed.
    const chosen = event.key === " " ? token : (options[highlight] ?? token);

    if ((event.key === "Tab" || event.key === "Enter" || event.key === " ") && chosen !== "") {
      event.preventDefault();
      commit(chosen);
    }
  };

  const clear = () => {
    change("");
    setTagFilter([], false);
  };

  return (
    <div className="mb-4 flex flex-wrap items-center gap-3">
      <div className="relative min-w-64 flex-1">
        <div className="flex min-h-9 w-full flex-wrap items-center gap-1 rounded-md border bg-background py-1 ps-9 pe-8 text-sm focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
          <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />

          {tags.map((tag) => (
            <span
              key={tag}
              className="inline-flex items-center gap-0.5 rounded border bg-muted/50 px-1.5 py-0.5 text-xs"
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
                className="rounded text-muted-foreground hover:text-foreground"
              >
                <X className="size-3" />
              </button>
            </span>
          ))}

          <input
            type="text"
            role="combobox"
            aria-label={placeholder}
            aria-describedby={hintId}
            aria-expanded={open}
            aria-controls={listboxId}
            aria-autocomplete="list"
            aria-activedescendant={open ? optionId(highlight) : undefined}
            aria-invalid={invalid || undefined}
            autoComplete="off"
            spellCheck={false}
            value={input}
            onChange={(event) => change(event.target.value)}
            onKeyDown={onKeyDown}
            onBlur={() => setDismissed(true)}
            placeholder={tags.length === 0 ? placeholder : undefined}
            className="min-w-32 flex-1 bg-transparent py-0.5 outline-none placeholder:text-muted-foreground aria-invalid:text-status-danger"
          />
        </div>

        <span
          id={hintId}
          className="sr-only"
        >
          Words search the text. Start a word with # for a tag: Tab or Enter completes it, Space
          keeps it as typed.
        </span>

        {(input !== "" || tags.length > 0) && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={clear}
            className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted-foreground outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <X className="size-3.5" />
          </button>
        )}

        <ul
          id={listboxId}
          role="listbox"
          aria-label="Tags in use"
          hidden={!open}
          className="absolute z-20 mt-1 max-h-60 w-64 overflow-y-auto rounded-md border bg-popover p-1 text-sm text-popover-foreground shadow-md"
        >
          {options.map((tag, index) => (
            // Pointed at through aria-activedescendant; focus stays in the input.
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events
            <li
              key={tag}
              id={optionId(index)}
              role="option"
              aria-selected={index === highlight}
              data-active={index === highlight || undefined}
              onMouseDown={(event) => event.preventDefault()}
              onMouseEnter={() => setHighlight(index)}
              onClick={() => commit(tag)}
              className="cursor-pointer rounded-sm px-2 py-1 data-active:bg-accent data-active:text-accent-foreground"
            >
              #{tag}
            </li>
          ))}
        </ul>

        {invalid && (
          <p
            role="alert"
            className="mt-1 text-xs text-status-danger"
          >
            A tag takes lowercase letters, digits, dots, dashes and underscores.
          </p>
        )}
      </div>

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
