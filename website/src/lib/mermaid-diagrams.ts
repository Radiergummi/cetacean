import type { Element, ElementContent, Parents, Root } from "hast";
import { create, type Font } from "fontkit";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { PeleError, render, type RenderOptions, type RenderResult, type TextMeasurer } from "pele";
import type { Plugin } from "unified";
import { visit } from "unist-util-visit";
import type { VFile } from "vfile";

/**
 * Mermaid diagrams, rendered to SVG at build time by Pele. Its colours are `--pele-*`
 * properties `global.css` points at the site's tokens, so the SVG follows the theme.
 * `mermaid` must stay in `markdown.syntaxHighlight.excludeLangs`: shiki runs ahead of
 * every user rehype plugin and would highlight the block before this sees it.
 */

const fontSize = 14;

/**
 * Each diagram is also laid out for a 375px phone's column less the canvas's
 * padding, where Pele turns a flowchart to run down and wraps a sequence diagram's
 * messages. It is kept only if clearly narrower, and shown once the wide layout
 * would draw below `smallestScale`: a breakpoint per diagram, not one for all.
 */
const narrowWidth = 300;
const smallestScale = 0.75;
const canvasPadding = 48;

/**
 * Pele bakes the box each label fits into the SVG, so it has to lay out with
 * the widths the browser will draw: Pele's regular weight, and the bold one
 * `global.css` sets through `--pele-bold-weight`. Keep the two in step.
 */
const fonts = {
  regular: loadFont(400),
  bold: loadFont(600),
};

function loadFont(weight: number): Font {
  const path = resolve(`node_modules/@fontsource/geist/files/geist-latin-${weight}-normal.woff`);

  return create(readFileSync(path)) as Font;
}

const boldStyle = 1;
const monospaceStyle = 4;

/**
 * Monospace is measured the way Pele's own fallback does, at 0.6em a character:
 * the site ships no monospace face for diagrams, so the reader's is unknown.
 */
const geistMeasurer: TextMeasurer = {
  width(text, size, style) {
    if (style & monospaceStyle) {
      return [...text].length * size * 0.6;
    }

    const font = style & boldStyle ? fonts.bold : fonts.regular;
    const { glyphs, positions } = font.layout(text);

    // A character Geist lacks, such as ①, is drawn in whatever fallback the reader
    // has. A full em is wider than any of those, so its box errs roomy, not tight.
    const units = glyphs.reduce(
      (total, { id }, index) =>
        total + (id === 0 ? font.unitsPerEm : (positions[index]?.xAdvance ?? 0)),
      0,
    );

    return (units * size) / font.unitsPerEm;
  },
};

/**
 * Astro's glob loader catches what `fail` throws, logs it, ships the page with its
 * body missing, and exits 0. So a build is brought down by hand — only a build: in
 * dev the message is what is wanted, not a dead server.
 */
function fail(file: VFile, message: string): never {
  if (process.argv.includes("build")) {
    console.error(message);
    process.exit(1);
  }

  file.fail(message);
}

/** The source of a mermaid code block, or `undefined` for any other element. */
function mermaidSource(node: Element): string | undefined {
  if (node.tagName !== "pre") {
    return undefined;
  }

  const code = node.children.find(
    (child): child is Element => "tagName" in child && child.tagName === "code",
  );

  if (!code?.properties?.className?.includes("language-mermaid")) {
    return undefined;
  }

  return code.children
    .map((child) => ("value" in child && child.value ? child.value : ""))
    .join("");
}

export const rehypeMermaid = function rehypeMermaid() {
  return (tree, file) => {
    let count = 0;

    visit(tree, "element", (node, index, parent: Parents | undefined) => {
      const source = mermaidSource(node);

      if (!parent || index === undefined || source === undefined) {
        return;
      }

      const id = `diagram-${count++}`;
      let wide: RenderResult;
      let narrow: RenderResult;

      try {
        wide = draw(source, `${id}-wide`);
        narrow = draw(source, `${id}-narrow`, { maxWidth: narrowWidth });
      } catch (error) {
        const position =
          error instanceof PeleError && error.line !== undefined ? ` (line ${error.line})` : "";

        fail(file, `Could not render mermaid diagram${position}: ${String(error)}\n\n${source}`);
      }

      // A parent is the root or an element, and TypeScript will not write to
      // the union of their two children arrays as one, though a figure is
      // content both of them hold.
      (parent.children as ElementContent[])[index] = figure(
        id,
        wide,
        narrow.width < wide.width * smallestScale ? narrow : undefined,
      );
    });
  };
} satisfies Plugin<[], Root, Root>;

function draw(source: string, idPrefix: string, options: RenderOptions = {}): RenderResult {
  return render(source, {
    measurer: geistMeasurer,
    fontSize,
    // Ids have to be unique across the page, not only within the diagram: the
    // title and description are referenced by them.
    idPrefix,
    // The stylesheet sizes the SVG; Pele's inline sizing would outrank it.
    responsive: false,
    ...options,
  });
}

function figure(id: string, wide: RenderResult, narrow: RenderResult | undefined): Element {
  return {
    type: "element",
    tagName: "figure",
    properties: { id, className: ["mermaid-figure"] },
    children: [
      {
        type: "element",
        tagName: "div",
        properties: { className: ["mermaid-viewport"] },
        children: [
          canvas(wide, "wide"),
          ...(narrow ? [canvas(narrow, "narrow"), switchAt(id, wide)] : []),
        ],
      },
    ],
  };
}

function canvas({ svg, width, height }: RenderResult, layout: string): Element {
  return {
    type: "element",
    // The element the enhancement script zooms. Pele reads the drawing from its
    // first child, so the SVG has to be exactly that.
    tagName: "div",
    properties: {
      className: ["mermaid-canvas"],
      dataLayout: layout,
      // The size Pele laid the diagram out at: never scaled past its width, and
      // fitted by its proportions in fullscreen.
      style: `--diagram-width: ${Math.ceil(width)}px; --diagram-ratio: ${width / height}`,
    },
    children: [{ type: "raw", value: svg } as unknown as Element],
  };
}

/**
 * Which layout shows is the viewport's container query, at the width below which
 * the wide one would draw under `smallestScale`. Important, so a layout for the
 * other width stays hidden whatever display the canvas takes, fullscreen included.
 */
function switchAt(id: string, wide: RenderResult): Element {
  const breakpoint = Math.ceil(wide.width * smallestScale + canvasPadding);
  const hide = (layout: string) => `#${id} [data-layout="${layout}"] { display: none !important; }`;

  return {
    type: "element",
    tagName: "style",
    properties: {},
    children: [
      {
        type: "text",
        value:
          `@container (width >= ${breakpoint}px) { ${hide("narrow")} } ` +
          `@container (width < ${breakpoint}px) { ${hide("wide")} }`,
      },
    ],
  };
}
