import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type React from "react";

const templateLabels: Record<string, string> = {
  ".Service.ID": "Service ID",
  ".Service.Name": "Service Name",
  ".Service.Labels": "Service Labels",
  ".Node.ID": "Node ID",
  ".Node.Hostname": "Node Hostname",
  ".Task.ID": "Task ID",
  ".Task.Name": "Task Name",
  ".Task.Slot": "Task Slot",
};

const templatePattern = /\{\{(\.(?:Service|Node|Task)\.(?:ID|Name|Hostname|Labels|Slot))\}\}/g;

/**
 * Replaces Docker Swarm Go template expressions (e.g. `{{.Node.Hostname}}`)
 * with styled badge elements. Returns the original string unchanged if it
 * contains no templates.
 *
 * Badge elements carry `data-copytext` with the raw template string.
 * Containers that render these badges should use {@link handleCopyWithTemplates}
 * as an `onCopy` handler to substitute display labels back to raw template
 * strings in the clipboard.
 */
export function renderSwarmTemplate(text: string): React.ReactNode {
  const parts: React.ReactNode[] = [];
  let lastIndex = 0;

  for (const match of text.matchAll(templatePattern)) {
    const before = text.slice(lastIndex, match.index);

    if (before) {
      parts.push(before);
    }

    const key = match[1];
    const raw = match[0];
    parts.push(
      <Tooltip key={match.index}>
        <TooltipTrigger
          render={
            <span
              className="inline-flex items-center rounded-md bg-violet-100 px-1.5 py-0.5 text-[11px] font-medium text-violet-800 dark:bg-violet-900/30 dark:text-violet-300"
              data-copytext={raw}
            >
              {templateLabels[key] ?? key}
            </span>
          }
        />
        <TooltipContent className="font-mono">{raw}</TooltipContent>
      </Tooltip>,
    );

    lastIndex = match.index + match[0].length;
  }

  if (lastIndex === 0) {
    return text;
  }

  const after = text.slice(lastIndex);

  if (after) {
    parts.push(after);
  }

  return parts;
}

/**
 * Copy Event handler that replaces `[data-copytext]` badge labels with their
 * raw template strings in the clipboard. Attach this to any container element
 * that renders swarm template badges.
 */
export function handleCopyWithTemplates(event: React.ClipboardEvent) {
  const selection = window.getSelection();

  if (!selection || selection.isCollapsed) {
    return;
  }

  const range = selection.getRangeAt(0);
  const fragment = range.cloneContents();

  if (!fragment.querySelector("[data-copytext]")) {
    return;
  }

  const wrapper = document.createElement("span");
  wrapper.appendChild(fragment);

  for (const badge of wrapper.querySelectorAll<HTMLElement>("[data-copytext]")) {
    badge.textContent = badge.dataset.copytext!;
  }

  event.clipboardData.setData("text/plain", wrapper.textContent ?? "");
  event.preventDefault();
}
