import type { LogLine } from "./log-utils";
import { isJSON, prettyJSON } from "./log-utils";

export function LogMessage({
  line,
  search,
  caseSensitive,
  useRegex,
  prettyJson,
}: {
  line: LogLine;
  search: string;
  caseSensitive: boolean;
  useRegex: boolean;
  prettyJson: boolean;
}) {
  const message = line.message;

  // If searching, highlight matches
  if (search) {
    const text = prettyJson && isJSON(message) ? prettyJSON(message) : message;

    return (
      <HighlightedText
        text={text}
        search={search}
        caseSensitive={caseSensitive}
        useRegex={useRegex}
      />
    );
  }

  // Auto-format JSON when pretty-printing is enabled
  if (isJSON(message)) {
    const text = prettyJson ? prettyJSON(message) : message;

    return <span className="text-emerald-700 dark:text-emerald-300">{text}</span>;
  }

  // Color error-level lines
  if (line.level === "error") {
    return <span className="text-red-600 dark:text-red-300">{message}</span>;
  }

  if (line.level === "warn") {
    return <span className="text-yellow-700 dark:text-yellow-300">{message}</span>;
  }

  if (line.level === "debug") {
    return <span className="text-muted-foreground">{message}</span>;
  }

  return <>{message}</>;
}

export function HighlightedText({
  text,
  search,
  caseSensitive,
  useRegex,
}: {
  text: string;
  search: string;
  caseSensitive: boolean;
  useRegex: boolean;
}) {
  const pattern = useRegex ? search : search.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  let expression: RegExp;

  try {
    expression = new RegExp(pattern, caseSensitive ? "g" : "gi");
  } catch {
    return <>{text}</>;
  }

  return (
    <>
      {highlightParts(text, expression).map(({ part, isMatch }, index) =>
        isMatch ? (
          <mark
            key={index}
            className="rounded-xs bg-yellow-200 px-px text-yellow-900 dark:bg-yellow-500/40 dark:text-yellow-200"
          >
            {part}
          </mark>
        ) : (
          <span key={index}>{part}</span>
        ),
      )}
    </>
  );
}

/**
 * Slices the text by match position, never by `split`, which splices the user's
 * own capture groups into the result. Zero-length matches are skipped.
 */
function highlightParts(text: string, expression: RegExp) {
  const parts: { part: string; isMatch: boolean }[] = [];
  let cursor = 0;

  for (const match of text.matchAll(expression)) {
    const [matched] = match;

    if (matched.length === 0) {
      continue;
    }

    if (match.index > cursor) {
      parts.push({ part: text.slice(cursor, match.index), isMatch: false });
    }

    parts.push({ part: matched, isMatch: true });
    cursor = match.index + matched.length;
  }

  if (cursor < text.length) {
    parts.push({ part: text.slice(cursor), isMatch: false });
  }

  return parts;
}
