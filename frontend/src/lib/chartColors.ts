/** IBM Carbon / Bang Wong CVD-safe palette — fallback values matching CSS --chart-* vars. */
export const CHART_COLORS = [
  "#648FFF", // Blue
  "#FFB000", // Gold
  "#DC267F", // Magenta
  "#785EF0", // Purple
  "#FE6100", // Orange
  "#02D4F5", // Cyan
  "#FFD966", // Amber
  "#CF9FFF", // Lavender
  "#FF85B3", // Pink
  "#47C1BF", // Teal
];

/** Cached resolved colors from CSS custom properties. */
let resolvedColors: string[] | null = null;

function resolveColors(): string[] {
  if (resolvedColors) return resolvedColors;
  if (typeof document === "undefined") return CHART_COLORS;
  const style = getComputedStyle(document.documentElement);
  resolvedColors = CHART_COLORS.map((fallback, i) => {
    const val = style.getPropertyValue(`--chart-${i + 1}`).trim();
    return val || fallback;
  });
  return resolvedColors;
}

/**
 * Get a chart color by index (wraps around).
 * Reads from CSS custom properties (cached after first call), falls back to hex constants.
 */
export function getChartColor(index: number): string {
  const colors = resolveColors();
  return colors[index % colors.length];
}

/** Cached semantic color resolutions. */
const semanticCache = new Map<string, string>();

/**
 * Get a semantic chart color by name (e.g. "cpu", "memory", "critical").
 * Reads from CSS custom property `--chart-{name}`, caches after first call.
 * For Chart.js contexts that need resolved hex/rgba values.
 */
export function getSemanticChartColor(name: string): string {
  const cached = semanticCache.get(name);
  if (cached) return cached;
  if (typeof document === "undefined") return "";
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(`--chart-${name}`)
    .trim();
  if (value) semanticCache.set(name, value);
  return value;
}
