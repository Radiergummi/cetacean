import { highlightLines, type Language } from "@/lib/highlight";

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

  if (/^\[[\w.-]+]/m.test(trimmed) && /^\w[\w.-]*\s*=/m.test(trimmed)) {
    return "toml";
  }

  if (/^\w[\w.-]*\s*:/m.test(trimmed)) {
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

const languages: Record<Exclude<Format, "plain">, Language> = {
  json: "json",
  yaml: "yaml",
  toml: "ini",
};

export default function CodeBlock({ code }: Props) {
  const format = detectFormat(code);
  const formatted = format === "json" ? tryPrettyJSON(code) : code;

  const lines =
    format === "plain" ? formatted.split("\n") : highlightLines(formatted, languages[format]);

  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="ps-3 pt-1.5">
        <span className="text-[10px] font-medium tracking-wider text-muted-foreground uppercase">
          {format}
        </span>
      </div>

      <pre className="overflow-x-auto py-2 font-mono text-sm break-all whitespace-pre-wrap">
        <code>
          {lines.map((line, index) => (
            <span
              key={index}
              className="block min-h-[1lh] px-4 even:bg-muted/50"
            >
              {line}
            </span>
          ))}
        </code>
      </pre>
    </div>
  );
}
