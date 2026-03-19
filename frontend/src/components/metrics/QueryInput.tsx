import { getTokenBounds } from "./useQueryCompletion";
import type { Suggestion } from "./useQueryCompletion";
import { Spinner } from "@/components/Spinner";
import { Play } from "lucide-react";
import { useRef, useEffect, useState, useCallback } from "react";

interface CompletionProps {
  suggestions: Suggestion[];
  loading: boolean;
  complete: (query: string, cursorPosition: number) => void;
  clear: () => void;
}

interface Props {
  value: string;
  onChange: (value: string) => void;
  onRun: () => void;
  loading: boolean;
  completion?: CompletionProps;
}

/**
 * PromQL query input with auto-resize textarea, Run button, and optional
 * autocompletion dropdown for metric names and PromQL functions.
 */
export function QueryInput({ value, onChange, onRun, loading, completion }: Props) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [highlightIndex, setHighlightIndex] = useState(0);
  const dropdownRef = useRef<HTMLUListElement>(null);

  const hasSuggestions = completion && completion.suggestions.length > 0;

  useEffect(() => {
    const textarea = textareaRef.current;

    if (!textarea) {
      return;
    }

    textarea.style.height = "auto";

    const lineHeight = parseInt(getComputedStyle(textarea).lineHeight, 10) || 20;
    const paddingTop = parseInt(getComputedStyle(textarea).paddingTop, 10) || 0;
    const paddingBottom = parseInt(getComputedStyle(textarea).paddingBottom, 10) || 0;
    const minHeight = lineHeight * 1 + paddingTop + paddingBottom;
    const maxHeight = lineHeight * 5 + paddingTop + paddingBottom;

    textarea.style.height = `${Math.min(Math.max(textarea.scrollHeight, minHeight), maxHeight)}px`;
  }, [value]);

  useEffect(() => {
    setHighlightIndex(0);
  }, [completion?.suggestions]);

  /**
   * Replaces the current token with the selected suggestion and updates the textarea.
   */
  const selectSuggestion = useCallback(
    (suggestion: Suggestion) => {
      const textarea = textareaRef.current;

      if (!textarea) {
        return;
      }

      const cursor = textarea.selectionStart;
      const { start, end } = getTokenBounds(value, cursor);
      const newValue = value.slice(0, start) + suggestion.label + value.slice(end);

      onChange(newValue);
      completion?.clear();

      const newCursor = start + suggestion.label.length;

      requestAnimationFrame(() => {
        textarea.focus();
        textarea.setSelectionRange(newCursor, newCursor);
      });
    },
    [value, onChange, completion],
  );

  const handleChange = (event: React.ChangeEvent<HTMLTextAreaElement>) => {
    const newValue = event.target.value;
    onChange(newValue);

    if (completion) {
      const cursor = event.target.selectionStart;
      completion.complete(newValue, cursor);
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const isModifier = event.metaKey || event.ctrlKey;

    if (isModifier && event.key === "Enter") {
      event.preventDefault();
      completion?.clear();
      onRun();
      return;
    }

    if (!hasSuggestions) {
      return;
    }

    const suggestions = completion.suggestions;

    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlightIndex((previous) => (previous < suggestions.length - 1 ? previous + 1 : 0));
      return;
    }

    if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlightIndex((previous) => (previous > 0 ? previous - 1 : suggestions.length - 1));
      return;
    }

    if (event.key === "Enter" || event.key === "Tab") {
      event.preventDefault();
      selectSuggestion(suggestions[highlightIndex]);
      return;
    }

    if (event.key === "Escape") {
      event.preventDefault();
      event.stopPropagation();
      completion.clear();
      return;
    }
  };

  const handleBlur = () => {
    // Delay clearing so click on suggestion can fire first
    setTimeout(() => {
      completion?.clear();
    }, 150);
  };

  useEffect(() => {
    if (!dropdownRef.current) {
      return;
    }

    const highlighted = dropdownRef.current.children[highlightIndex] as HTMLElement | undefined;

    if (highlighted) {
      highlighted.scrollIntoView({ block: "nearest" });
    }
  }, [highlightIndex]);

  return (
    <div className="relative flex gap-2">
      <div className="relative flex-1">
        <textarea
          ref={textareaRef}
          value={value}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          placeholder="Enter a PromQL expression..."
          rows={1}
          className="w-full resize-none overflow-hidden rounded-md border bg-background px-3 py-1.5 font-mono text-sm placeholder:text-muted-foreground focus:ring-1 focus:ring-ring focus:outline-none"
        />

        {hasSuggestions && (
          <ul
            ref={dropdownRef}
            className="absolute top-full left-0 z-50 mt-1 max-h-60 w-full overflow-auto rounded-md border bg-popover py-1 shadow-md"
          >
            {completion.suggestions.map((suggestion, index) => (
              <li
                key={suggestion.label}
                className={`flex cursor-pointer items-center gap-2 px-3 py-1.5 text-sm ${
                  index === highlightIndex ? "bg-accent text-accent-foreground" : ""
                }`}
                onMouseDown={(event) => {
                  event.preventDefault();
                  selectSuggestion(suggestion);
                }}
                onMouseEnter={() => setHighlightIndex(index)}
              >
                <span className="flex-1 truncate font-mono">{suggestion.label}</span>

                {suggestion.type === "function" ? (
                  <span className="shrink-0 rounded bg-blue-500/10 px-1.5 py-0.5 text-[10px] font-medium text-blue-600 dark:text-blue-400">
                    fn
                  </span>
                ) : (
                  <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                    metric
                  </span>
                )}

                {suggestion.detail && (
                  <span className="hidden shrink-0 truncate text-xs text-muted-foreground sm:block">
                    {suggestion.detail}
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>

      <button
        onClick={onRun}
        disabled={loading}
        className="inline-flex h-8 items-center gap-1.5 self-start rounded-md border bg-background px-2.5 text-xs hover:bg-muted disabled:opacity-50"
        title="Run query (Cmd+Enter)"
      >
        {loading ? <Spinner className="size-3.5" /> : <Play className="size-3.5" />}
        Run
      </button>
    </div>
  );
}
