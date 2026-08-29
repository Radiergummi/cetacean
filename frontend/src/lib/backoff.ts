export interface BackoffOptions {
  /** Delay before the first retry, in milliseconds. */
  base: number;
  /** Ceiling the doubling never exceeds, in milliseconds. */
  max: number;
}

/**
 * Exponential backoff: the first retry waits `base`, each subsequent one
 * doubles, and none waits longer than `max`.
 *
 * The schedule is shared; the values are not. Each stream passes its own
 * `base` and `max` because the policies genuinely differ — the log tail
 * starts fast because a person is waiting, background streams start at the
 * interval the server asks for in its Retry-After.
 *
 * @param attempt 1-based index of the retry about to be made.
 */
export function backoffDelay(attempt: number, { base, max }: BackoffOptions): number {
  return Math.min(base * 2 ** (Math.max(1, attempt) - 1), max);
}
