import { createMermaidRenderer, type RenderResult } from "mermaid-isomorphic";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import type { Element } from "hast";
import type { Plugin } from "unified";
import { visit } from "unist-util-visit";

/**
 * Mermaid diagrams, rendered to SVG at build time.
 *
 * The site's dark mode is a class on the root element, toggled at runtime, so a
 * single build-time SVG cannot follow it — mermaid bakes its palette into a
 * `<style>` block scoped to the diagram's own ID. Rendering the diagram twice
 * and letting CSS pick one costs a few KB of markup per diagram and keeps the
 * page correct in both themes with no JavaScript at all, which shipping mermaid
 * to the browser would not: the client bundle is over half a megabyte, and a
 * reader without JS would see nothing.
 *
 * Interactivity is a separate concern layered on top of the SVG by
 * `MermaidViewport.astro`, so the diagram is readable before that script runs
 * and remains readable if it never does.
 *
 * The `mermaid` language must be listed in `markdown.syntaxHighlight.excludeLangs`,
 * or shiki — which runs ahead of every user rehype plugin — turns the block into
 * a highlighted `<pre>` before this ever sees it.
 */

const fontFamily = '"Geist Variable", ui-sans-serif, system-ui, sans-serif';

/**
 * Shared by both themes. Mermaid derives a good deal of its palette from these
 * by lightening and darkening, so only the anchors are set.
 */
const baseTheme = {
  fontFamily,
  fontSize: "14px",
  lineColor: "#71717a",
};

const themes = {
  light: {
    ...baseTheme,
    background: "transparent",
    primaryColor: "#f4f4f5",
    primaryTextColor: "#18181b",
    primaryBorderColor: "#d4d4d8",
    secondaryColor: "#fafafa",
    secondaryBorderColor: "#e4e4e7",
    textColor: "#3f3f46",
    clusterBkg: "#fafafa",
    clusterBorder: "#e4e4e7",
    tertiaryColor: "#fafafa",
    tertiaryBorderColor: "#e4e4e7",
    tertiaryTextColor: "#3f3f46",
    edgeLabelBackground: "#fafafa",
    // Sequence diagrams derive almost nothing from the anchors above; left
    // alone they come out with mermaid's default yellow notes.
    actorBkg: "#f4f4f5",
    actorBorder: "#d4d4d8",
    actorTextColor: "#18181b",
    actorLineColor: "#a1a1aa",
    signalColor: "#71717a",
    signalTextColor: "#3f3f46",
    noteBkgColor: "#fafafa",
    noteBorderColor: "#e4e4e7",
    noteTextColor: "#3f3f46",
    labelBoxBkgColor: "#f4f4f5",
    labelBoxBorderColor: "#d4d4d8",
    labelTextColor: "#18181b",
    activationBkgColor: "#e4e4e7",
    activationBorderColor: "#d4d4d8",
    sequenceNumberColor: "#ffffff",
  },
  dark: {
    ...baseTheme,
    background: "transparent",
    primaryColor: "#27272a",
    primaryTextColor: "#f4f4f5",
    primaryBorderColor: "#3f3f46",
    secondaryColor: "#1f1f22",
    secondaryBorderColor: "#33333a",
    textColor: "#d4d4d8",
    lineColor: "#a1a1aa",
    clusterBkg: "#1c1c1f",
    clusterBorder: "#33333a",
    tertiaryColor: "#1c1c1f",
    tertiaryBorderColor: "#33333a",
    tertiaryTextColor: "#d4d4d8",
    edgeLabelBackground: "#1b1b1b",
    actorBkg: "#27272a",
    actorBorder: "#3f3f46",
    actorTextColor: "#f4f4f5",
    actorLineColor: "#52525b",
    signalColor: "#a1a1aa",
    signalTextColor: "#d4d4d8",
    noteBkgColor: "#1c1c1f",
    noteBorderColor: "#33333a",
    noteTextColor: "#d4d4d8",
    labelBoxBkgColor: "#27272a",
    labelBoxBorderColor: "#3f3f46",
    labelTextColor: "#f4f4f5",
    activationBkgColor: "#3f3f46",
    activationBorderColor: "#52525b",
    sequenceNumberColor: "#18181b",
  },
};

const fontCss = pathToFileURL(resolve("src/lib/mermaid-fonts.css"));

/**
 * Mermaid sizes the root `<svg>` with an inline `max-width`, which would win
 * over any stylesheet. Dropping it lets the viewport own the sizing.
 */
function unpinWidth(svg: string): string {
  return svg.replace(/^(<svg[^>]*?)\s+style="[^"]*"/, "$1");
}

/**
 * Mermaid puts label text in an HTML `<p>` inside a `<foreignObject>`, and
 * writes a `<br/>` for every line break. rehype-raw reparses what this plugin
 * emits, and it never leaves SVG space on the way into a foreignObject, so `br`
 * is not treated as void: `<br/>` comes back as `<br></br>`, which HTML parses
 * as *two* breaks, pushing the last line out of the box mermaid measured.
 *
 * Ending the paragraph and opening the next one breaks the line without a void
 * element. Mermaid's own stylesheet carries `p { margin: 0 }`, so the two
 * render exactly alike.
 */
function splitLineBreaks(svg: string): string {
  return svg.replace(/<br\s*\/?>/g, "</p><p>");
}

function isMermaidBlock(node: Element): boolean {
  if (node.tagName !== "pre") {
    return false;
  }

  const code = node.children.find(
    (child): child is Element => "tagName" in child && child.tagName === "code",
  );

  return Boolean(code?.properties?.className?.includes("language-mermaid"));
}

function sourceOf(node: Element) {
  const code = node.children.find(
    (child): child is Element => "tagName" in child && child.tagName === "code",
  );

  return code!.children
    .map((child) => ("value" in child && child.value ? child.value : ""))
    .join("");
}

export const rehypeMermaid = function rehypeMermaid() {
  const render = createMermaidRenderer();

  return async (tree, file) => {
    const blocks: { parent: Element; index: number; source: string }[] = [];

    visit(tree, "element", (node, index, parent) => {
      if (parent && index !== undefined && isMermaidBlock(node)) {
        blocks.push({ parent, index, source: sourceOf(node) });
      }
    });

    if (blocks.length === 0) {
      return;
    }

    const sources = blocks.map(({ source }) => source);
    const [light, dark] = await Promise.all(
      Object.entries(themes).map(([name, themeVariables]) =>
        render(sources, {
          css: fontCss,
          prefix: `mermaid-${name}`,
          mermaidConfig: { theme: "base", themeVariables, fontFamily },
        }),
      ),
    );

    blocks.forEach((block, index) => {
      const lightResult = light[index];
      const darkResult = dark[index];

      if (lightResult.status === "rejected" || darkResult.status === "rejected") {
        const reason =
          lightResult.status === "rejected"
            ? lightResult.reason
            : darkResult.status === "rejected"
              ? darkResult.reason
              : "unknown error";
        file.fail(`Could not render mermaid diagram: ${reason}`);
      } else {
        block.parent.children[block.index] = figure(lightResult.value, darkResult.value);
      }
    });
  };
} satisfies Plugin<[], Element, Element>;

/**
 * The dark copy is hidden from Pagefind: both carry the same label text, and
 * indexing each diagram twice would surface duplicate hits for one page.
 */
function figure(light: RenderResult, dark: RenderResult): Element {
  const label = light.title ?? light.description ?? "Diagram";

  return {
    type: "element",
    tagName: "figure",
    properties: {
      className: ["mermaid-figure"],
      "aria-label": label,
      role: "img",
      // The width mermaid laid the diagram out at. The stylesheet scales down
      // to fit the column but stops at a legibility floor and scrolls past it,
      // and never scales a small diagram up.
      style: `--diagram-width: ${Math.ceil(Math.max(light.width, dark.width))}px`,
    },
    children: [
      {
        type: "element",
        tagName: "div",
        properties: { className: ["mermaid-viewport"] },
        children: [
          {
            type: "element",
            // One element for the enhancement script to transform, so panning
            // does not have to be kept in step across the two theme copies.
            tagName: "div",
            properties: { className: ["mermaid-canvas"] },
            children: [
              {
                type: "element",
                tagName: "div",
                properties: { className: ["mermaid-svg", "mermaid-light"] },
                children: [
                  {
                    type: "raw",
                    value: splitLineBreaks(unpinWidth(light.svg)),
                  } as unknown as Element,
                ],
              },
              {
                type: "element",
                tagName: "div",
                properties: {
                  className: ["mermaid-svg", "mermaid-dark"],
                  "data-pagefind-ignore": true,
                },
                children: [
                  {
                    type: "raw",
                    value: splitLineBreaks(unpinWidth(dark.svg)),
                  } as unknown as Element,
                ],
              },
            ],
          },
        ],
      },
    ],
  };
}
