import type React from "react";

interface Props {
  code: string;
}

type Format = "json" | "yaml" | "toml" | "plain";

function detectFormat(text: string): Format {
  const trimmed = text.trim();
  if (
    (trimmed.startsWith("{") && trimmed.endsWith("}")) ||
    (trimmed.startsWith("[") && trimmed.endsWith("]"))
  ) {
    try {
      JSON.parse(trimmed);
      return "json";
    } catch {
      // not valid JSON
    }
  }
  if (/^\[[\w.-]+\]/m.test(trimmed) && /^\w[\w.-]*\s*=/m.test(trimmed)) {
    return "toml";
  }
  if (/^[\w][\w.-]*\s*:/m.test(trimmed)) {
    return "yaml";
  }
  return "plain";
}

function tryPrettyJSON(code: string): string {
  try {
    return JSON.stringify(JSON.parse(code), null, 2);
  } catch {
    return code;
  }
}

function highlightJSON(code: string): React.ReactNode[] {
  const regex =
    /("(?:[^"\\]|\\.)*")(\s*:)?|(\b(?:true|false|null)\b)|(-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b)/g;
  const parts: React.ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  let key = 0;

  while ((match = regex.exec(code)) !== null) {
    if (match.index > lastIndex) {
      parts.push(<span key={key++}>{code.slice(lastIndex, match.index)}</span>);
    }

    if (match[1]) {
      if (match[2]) {
        parts.push(
          <span
            key={key++}
            className="text-sky-700 dark:text-sky-300"
          >
            {match[1]}
          </span>,
        );
        parts.push(<span key={key++}>{match[2]}</span>);
      } else {
        parts.push(
          <span
            key={key++}
            className="text-green-700 dark:text-green-400"
          >
            {match[1]}
          </span>,
        );
      }
    } else if (match[3]) {
      parts.push(
        <span
          key={key++}
          className="text-purple-700 dark:text-purple-400"
        >
          {match[3]}
        </span>,
      );
    } else if (match[4]) {
      parts.push(
        <span
          key={key++}
          className="text-amber-700 dark:text-amber-400"
        >
          {match[4]}
        </span>,
      );
    }

    lastIndex = regex.lastIndex;
  }

  if (lastIndex < code.length) {
    parts.push(<span key={key++}>{code.slice(lastIndex)}</span>);
  }

  return parts;
}

function highlightYAML(code: string): React.ReactNode[] {
  return code.split("\n").map((line, index) => {
    if (line.trimStart().startsWith("#")) {
      return (
        <span key={index}>
          <span className="text-muted-foreground italic">{line}</span>
          {"\n"}
        </span>
      );
    }

    const match = line.match(/^(\s*)(\w[\w.-]*)(\s*:\s?)(.*)/);

    if (match) {
      const [, indent, key, colon, value] = match;

      return (
        <span key={index}>
          {indent}
          <span className="text-sky-700 dark:text-sky-300">{key}</span>
          {colon}
          {value && <span className="text-green-700 dark:text-green-400">{value}</span>}
          {"\n"}
        </span>
      );
    }
    return (
      <span key={index}>
        {line}
        {"\n"}
      </span>
    );
  });
}

export default function CodeBlock({ code }: Props) {
  const format = detectFormat(code);
  const formatted = format === "json" ? tryPrettyJSON(code) : code;

  let highlighted: React.ReactNode;

  if (format === "json") {
    highlighted = highlightJSON(formatted);
  } else if (format === "yaml") {
    highlighted = highlightYAML(formatted);
  } else {
    highlighted = formatted;
  }

  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="ps-3 pt-1.5">
        <span className="text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
          {format}
        </span>
      </div>

      <pre className="overflow-x-auto p-4 font-mono text-sm break-all whitespace-pre-wrap">
        <code>{highlighted}</code>
      </pre>
    </div>
  );
}
