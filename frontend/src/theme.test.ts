import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "index.css"), "utf8");

type RGB = readonly [number, number, number];

function tokens(selector: string): Map<string, RGB> {
  const escaped = selector.replace(".", "\\.");
  const block = css.match(new RegExp(`^${escaped} \\{([^}]*)\\}`, "m"))?.[1] ?? "";
  const result = new Map<string, RGB>();

  for (const [, name = "", l = "", c = "", h = ""] of block.matchAll(
    /--([\w-]+):\s*oklch\(([\d.]+) ([\d.]+) ([\d.]+)\)/g,
  )) {
    result.set(name, oklchToSrgb(Number(l), Number(c), Number(h)));
  }

  return result;
}

function oklchToSrgb(lightness: number, chroma: number, hue: number): RGB {
  const a = chroma * Math.cos((hue * Math.PI) / 180);
  const b = chroma * Math.sin((hue * Math.PI) / 180);
  const l = (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (lightness - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const encode = (value: number) => {
    const clipped = Math.min(1, Math.max(0, value));

    return clipped <= 0.0031308 ? 12.92 * clipped : 1.055 * clipped ** (1 / 2.4) - 0.055;
  };

  return [
    encode(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s),
    encode(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s),
    encode(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s),
  ];
}

function luminance([r, g, b]: RGB): number {
  const decode = (value: number) =>
    value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4;

  return 0.2126 * decode(r) + 0.7152 * decode(g) + 0.0722 * decode(b);
}

function contrast(foreground: RGB, background: RGB): number {
  const high = Math.max(luminance(foreground), luminance(background));
  const low = Math.min(luminance(foreground), luminance(background));

  return (high + 0.05) / (low + 0.05);
}

const light = tokens(":root");
const dark = new Map([...light, ...tokens(".dark")]);

// WCAG 1.4.3 AA for body-size text.
const pairs: [foreground: string, background: string][] = [
  ["muted-foreground", "background"],
  ["muted-foreground", "muted"],
  ["muted-foreground", "card"],
  ["status-foreground", "status-ok"],
  ["status-foreground", "status-warning"],
  ["status-foreground", "status-danger"],
  ["status-foreground", "status-info"],
  ["status-foreground", "status-neutral"],
];

describe.each([
  ["light", light],
  ["dark", dark],
])("%s theme tokens", (_, theme) => {
  it.each(pairs)("%s on %s meets 4.5:1", (foreground, background) => {
    const fg = theme.get(foreground);
    const bg = theme.get(background);

    if (!fg || !bg) {
      throw new Error(`--${fg ? background : foreground} is not an oklch() token`);
    }

    expect(contrast(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });
});
