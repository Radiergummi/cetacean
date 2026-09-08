import { createMermaidRenderer, type RenderResult } from "mermaid-isomorphic";
import { resolve } from "node:path";
import { pathToFileURL } from "node:url";
import type { Element, ElementContent, Parents, Root } from "hast";
import type { Plugin } from "unified";
import type { VFile } from "vfile";
import { visit } from "unist-util-visit";

/**
 * Mermaid diagrams, rendered to SVG at build time.
 *
 * Mermaid resolves colours as it renders, in a headless browser that never loads
 * this site's stylesheet, and bakes the result into a `<style>` block scoped to
 * the diagram's own ID — so a diagram rendered in the site's colours cannot
 * follow a dark mode that is a class toggled on the root element at runtime.
 * What mermaid is *given* has to be a real colour, since khroma derives the rest
 * of the palette from it and `var(--x)` is not one; what mermaid *emits* is only
 * text. So it is given a sentinel per role, and the sentinels are swapped for
 * custom properties once the SVG comes back. One copy then follows the theme
 * with no JavaScript at all, which shipping mermaid to the browser would not:
 * the client bundle is over half a megabyte, and a reader without JS would see
 * nothing.
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
 * Every colour mermaid is given, mapped to the custom property that replaces it
 * in the rendered SVG. The values live in `global.css` beside the site's own
 * tokens, which is the point of the exercise: the palette is no longer a second
 * copy of them kept over here.
 *
 * Mermaid derives a good deal from the first few anchors, but almost nothing of
 * what a sequence diagram draws — left alone, its notes come out mermaid's
 * default yellow — which is why most of this list is spelled out.
 */
const roleVariables = {
  primaryColor: "--mermaid-primary",
  primaryTextColor: "--mermaid-primary-text",
  primaryBorderColor: "--mermaid-primary-border",
  secondaryColor: "--mermaid-secondary",
  secondaryBorderColor: "--mermaid-secondary-border",
  textColor: "--mermaid-text",
  lineColor: "--mermaid-line",
  clusterBkg: "--mermaid-cluster",
  clusterBorder: "--mermaid-cluster-border",
  tertiaryColor: "--mermaid-tertiary",
  tertiaryBorderColor: "--mermaid-tertiary-border",
  tertiaryTextColor: "--mermaid-tertiary-text",
  edgeLabelBackground: "--mermaid-edge-label",
  actorBkg: "--mermaid-actor",
  actorBorder: "--mermaid-actor-border",
  actorTextColor: "--mermaid-actor-text",
  actorLineColor: "--mermaid-actor-line",
  signalColor: "--mermaid-signal",
  signalTextColor: "--mermaid-signal-text",
  noteBkgColor: "--mermaid-note",
  noteBorderColor: "--mermaid-note-border",
  noteTextColor: "--mermaid-note-text",
  labelBoxBkgColor: "--mermaid-label-box",
  labelBoxBorderColor: "--mermaid-label-box-border",
  labelTextColor: "--mermaid-label-text",
  activationBkgColor: "--mermaid-activation",
  activationBorderColor: "--mermaid-activation-border",
  sequenceNumberColor: "--mermaid-sequence-number",
};

const roles = Object.keys(roleVariables) as (keyof typeof roleVariables)[];

/**
 * One unmistakable colour per role. Unique, so the swap cannot confuse two roles
 * that resolve to the same colour — four of them did, back when these were real
 * greys — and far enough outside any palette that a diagram setting one by hand
 * would be a coincidence worth looking into anyway.
 */
function sentinel(index: number): string {
  return `#fe${index.toString(16).padStart(4, "0")}`;
}

const themeVariables = {
  fontFamily,
  fontSize: "14px",
  background: "transparent",
  ...Object.fromEntries(roles.map((role, index) => [role, sentinel(index)])),
};

const fontCss = pathToFileURL(resolve("src/lib/mermaid-fonts.css"));

/**
 * Mermaid writes most colours as the hex it was handed, but round-trips some
 * through the browser's computed styles, which come back as `rgb()`, and gives a
 * few an alpha of their own — the edge label backing is its themed colour at half
 * opacity. A custom property cannot carry that alpha, so `color-mix` reapplies it.
 */
function swap(svg: string, hex: string, variable: string): string {
  const [red, green, blue] = [1, 3, 5].map((at) => parseInt(hex.slice(at, at + 2), 16));

  const spellings = new RegExp(
    `${hex}\\b|rgba?\\(\\s*${red},\\s*${green},\\s*${blue}\\s*(?:,\\s*([\\d.]+)\\s*)?\\)`,
    "gi",
  );

  return svg.replace(spellings, (_color: string, alpha?: string) => {
    const opacity = alpha === undefined ? 1 : Number(alpha);

    return opacity === 1
      ? `var(${variable})`
      : `color-mix(in srgb, var(${variable}) ${opacity * 100}%, transparent)`;
  });
}

function applyTheme(svg: string): string {
  return roles.reduce(
    (themed, role, index) => swap(themed, sentinel(index), roleVariables[role]),
    svg,
  );
}

/** `#abc`, `rgb(1, 2, 3)` and `rgba(1, 2, 3, .5)` all reduce to `1,2,3`. */
function canonical(color: string): string | undefined {
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(color);

  if (hex) {
    const digits = hex[1].length === 3 ? hex[1].replace(/./g, "$&$&") : hex[1];

    return [0, 2, 4].map((at) => parseInt(digits.slice(at, at + 2), 16)).join(",");
  }

  const channels = /^rgba?\(([^)]*)\)$/i.exec(color);

  return channels
    ? channels[1]
        .split(/[\s,/]+/)
        .filter(Boolean)
        .slice(0, 3)
        .map(Number)
        .join(",")
    : undefined;
}

const colorPattern = /#[0-9a-f]{3}\b|#[0-9a-f]{6}\b|rgba?\([^)]*\)/gi;

/**
 * Every sentinel is a saturated red and khroma's lightening and darkening hold
 * the hue, so a red-dominant colour left in the SVG came from one. Mermaid's own
 * hard-coded constants are greys and one olive note fill, and a diagram's own
 * colours are read from its source, so neither is mistaken for a derivation.
 *
 * A derivation pale enough to stop being red-dominant would slip through. That is
 * the limit of a cheap check, not a claim to be exhaustive — and a near-white
 * patch is a milder way to be wrong than the lurid one this is here to catch.
 */
function fromSentinel(channels: string): boolean {
  const [red, green, blue] = channels.split(",").map(Number);

  return red > 60 && red > green * 2 && red > blue * 2;
}

function assertThemed(svg: string, source: string, file: VFile): void {
  const authored = new Set([...source.matchAll(colorPattern)].map((match) => canonical(match[0])));

  const strays = [...new Set([...svg.matchAll(colorPattern)].map((match) => match[0]))].filter(
    (color) => {
      const key = canonical(color);

      return key !== undefined && fromSentinel(key) && !authored.has(key);
    },
  );

  if (strays.length === 0) {
    return;
  }

  const message =
    `Mermaid derived ${strays.join(", ")} from a themed colour, so this diagram would not ` +
    `follow the site's theme. Add the role it came from to \`roleVariables\`.\n\n${source}`;

  // Astro's glob loader catches what `fail` throws, logs it, and ships the page with
  // its body missing rather than stopping — and it exits 0 either way, even with
  // `process.exitCode` set. So a build has to be brought down by hand, and only a
  // build: in dev the message is what is wanted, not a dead server.
  if (process.argv.includes("build")) {
    console.error(message);
    process.exit(1);
  }

  file.fail(message);
}

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
    const blocks: { parent: Parents; index: number; source: string }[] = [];

    visit(tree, "element", (node, index, parent) => {
      if (parent && index !== undefined && isMermaidBlock(node)) {
        blocks.push({ parent, index, source: sourceOf(node) });
      }
    });

    if (blocks.length === 0) {
      return;
    }

    const rendered = await render(
      blocks.map(({ source }) => source),
      {
        css: fontCss,
        prefix: "mermaid",
        mermaidConfig: { theme: "base", themeVariables, fontFamily },
      },
    );

    blocks.forEach((block, index) => {
      const result = rendered[index];

      if (result.status === "rejected") {
        file.fail(`Could not render mermaid diagram: ${result.reason}`);
      } else {
        const svg = applyTheme(splitLineBreaks(unpinWidth(result.value.svg)));

        assertThemed(svg, block.source, file);
        // A parent is the root or an element, and TypeScript will not write to
        // the union of their two children arrays as one, though a figure is
        // content both of them hold.
        (block.parent.children as ElementContent[])[block.index] = figure(result.value, svg);
      }
    });
  };
} satisfies Plugin<[], Root, Root>;

function figure(diagram: RenderResult, svg: string): Element {
  const label = diagram.title ?? diagram.description ?? "Diagram";

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
      style: `--diagram-width: ${Math.ceil(diagram.width)}px`,
    },
    children: [
      {
        type: "element",
        tagName: "div",
        properties: { className: ["mermaid-viewport"] },
        children: [
          {
            type: "element",
            // The element the enhancement script transforms to pan the diagram.
            tagName: "div",
            properties: { className: ["mermaid-canvas"] },
            children: [{ type: "raw", value: svg } as unknown as Element],
          },
        ],
      },
    ],
  };
}
