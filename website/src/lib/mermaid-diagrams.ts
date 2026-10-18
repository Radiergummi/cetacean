import type { Element, ElementContent, Parents, Root } from "hast";
import { create, type Font } from "fontkit";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { PeleError, render, type RenderResult, type TextMeasurer } from "pele";
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
 * Pele bakes the box each label fits into the SVG, so it has to lay out with
 * the widths the browser will draw. These are static cuts of the weights
 * `global.css` sets diagram text in; the two have to be kept in step.
 */
const fonts = {
  regular: loadFont(500),
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

      let diagram: RenderResult;

      try {
        diagram = render(source, {
          measurer: geistMeasurer,
          fontSize,
          // Ids have to be unique across the page, not only within the diagram:
          // the title and description are referenced by them.
          idPrefix: `diagram-${count++}`,
          // The stylesheet sizes the SVG; Pele's inline sizing would outrank it.
          responsive: false,
        });
      } catch (error) {
        const position =
          error instanceof PeleError && error.line !== undefined ? ` (line ${error.line})` : "";

        fail(file, `Could not render mermaid diagram${position}: ${String(error)}\n\n${source}`);
      }

      // A parent is the root or an element, and TypeScript will not write to
      // the union of their two children arrays as one, though a figure is
      // content both of them hold.
      (parent.children as ElementContent[])[index] = figure(diagram);
    });
  };
} satisfies Plugin<[], Root, Root>;

function figure(diagram: RenderResult): Element {
  return {
    type: "element",
    tagName: "figure",
    properties: {
      className: ["mermaid-figure"],
      // The width Pele laid the diagram out at. The stylesheet scales down to
      // fit the column but stops at a legibility floor and scrolls past it,
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
            // The element the enhancement script zooms. Pele reads the drawing
            // from its first child, so the SVG has to be exactly that.
            tagName: "div",
            properties: { className: ["mermaid-canvas"] },
            children: [{ type: "raw", value: diagram.svg } as unknown as Element],
          },
        ],
      },
    ],
  };
}
