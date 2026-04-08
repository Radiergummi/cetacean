export const durationUnits = [
  { label: "seconds", factor: 1_000_000_000 },
  { label: "minutes", factor: 60_000_000_000 },
  { label: "hours", factor: 3_600_000_000_000 },
  { label: "days", factor: 86_400_000_000_000 },
] as const;

export type DurationUnit = (typeof durationUnits)[number];

/**
 * Find the largest duration unit that evenly divides the given nanosecond value.
 */
export function bestDurationUnit(nanoseconds: number): DurationUnit {
  for (let index = durationUnits.length - 1; index > 0; index--) {
    if (
      nanoseconds >= durationUnits[index].factor &&
      nanoseconds % durationUnits[index].factor === 0
    ) {
      return durationUnits[index];
    }
  }

  return durationUnits[0];
}
