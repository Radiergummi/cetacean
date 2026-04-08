/**
 * Compute CPU usage as a 0–100 gauge percentage.
 *
 * @param usagePercent CPU usage as a percentage of 1 vCPU (e.g., 150 = 1.5 cores).
 * @param limitNanoCpus CPU limit in Docker nanosecond units (1e9 = 1 core).
 */
export function cpuGaugePercent(
  usagePercent: number | null,
  limitNanoCpus: number | undefined | null,
): number | null {
  if (usagePercent == null || !limitNanoCpus) {
    return null;
  }

  // 1e9 nano = 1 core = 100%, so limitNano / 1e7 converts to the same unit as usagePercent.
  return usagePercent / (limitNanoCpus / 1e7);
}

/**
 * Compute memory usage as a 0–100 gauge percentage.
 *
 * @param usageBytes Current memory usage in bytes.
 * @param limitBytes Memory limit in bytes.
 */
export function memoryGaugePercent(
  usageBytes: number | null,
  limitBytes: number | undefined | null,
): number | null {
  if (usageBytes == null || !limitBytes) {
    return null;
  }

  return (usageBytes / limitBytes) * 100;
}
