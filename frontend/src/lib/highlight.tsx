import bash from "highlight.js/lib/languages/bash";
import dockerfile from "highlight.js/lib/languages/dockerfile";
import ini from "highlight.js/lib/languages/ini";
import json from "highlight.js/lib/languages/json";
import yaml from "highlight.js/lib/languages/yaml";
import { createLowlight } from "lowlight";
import { Fragment, type ReactNode } from "react";

// A Dockerfile's RUN, CMD and ENTRYPOINT are highlighted as bash, and TOML as ini.
const lowlight = createLowlight({ bash, dockerfile, ini, json, yaml });

export type Language = "bash" | "dockerfile" | "ini" | "json" | "yaml";

const scopeClasses: Record<string, string> = {
  "hljs-keyword": "text-purple-700 dark:text-purple-400",
  "hljs-built_in": "text-purple-700 dark:text-purple-400",
  "hljs-section": "text-purple-700 dark:text-purple-400",
  "hljs-attr": "text-sky-700 dark:text-sky-300",
  "hljs-variable": "text-sky-700 dark:text-sky-300",
  "hljs-template-variable": "text-sky-700 dark:text-sky-300",
  "hljs-string": "text-green-700 dark:text-green-400",
  "hljs-number": "text-amber-700 dark:text-amber-400",
  "hljs-literal": "text-amber-700 dark:text-amber-400",
  "hljs-comment": "text-muted-foreground italic",
  "hljs-meta": "text-muted-foreground",
};

interface Node {
  type: string;
  value?: string;
  properties?: { className?: unknown };
  children?: Node[];
}

interface Segment {
  text: string;
  className: string | undefined;
}

function segments(node: Node, className: string | undefined, out: Segment[]) {
  if (node.type === "text") {
    out.push({ text: node.value ?? "", className });

    return;
  }

  const names = node.properties?.className;
  const own = Array.isArray(names)
    ? names.map((name) => scopeClasses[String(name)]).find(Boolean)
    : undefined;

  for (const child of node.children ?? []) {
    segments(child, own ?? className, out);
  }
}

/**
 * Highlights code and returns it one node per line, so a caller can number
 * the lines or lay them out in a table. A token spanning lines is split.
 */
export function highlightLines(code: string, language: Language): ReactNode[] {
  const flat: Segment[] = [];
  segments(lowlight.highlight(language, code) as Node, undefined, flat);

  const lines: ReactNode[][] = [[]];

  flat.forEach(({ text, className }, index) => {
    text.split("\n").forEach((part, partIndex) => {
      if (partIndex > 0) {
        lines.push([]);
      }

      if (part) {
        lines[lines.length - 1]?.push(
          className ? (
            <span
              key={`${index}-${partIndex}`}
              className={className}
            >
              {part}
            </span>
          ) : (
            <Fragment key={`${index}-${partIndex}`}>{part}</Fragment>
          ),
        );
      }
    });
  });

  return lines.map((parts, index) => <Fragment key={index}>{parts}</Fragment>);
}
