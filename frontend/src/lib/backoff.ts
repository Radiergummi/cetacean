export interface BackoffOptions {
  /** Delay before the first retry, in milliseconds. */
  base: number;
  /**
   * Ceiling the doubling never exceeds, in milliseconds. It bounds the nominal
   * schedule; jitter is applied on top, so an individual delay may land up to
   * `backoffJitterFraction` above it.
   */
  max: number;
}

/**
 * How far a jittered delay may fall either side of its nominal value.
 *
 * Clients that fail together compute the same delay, so without a spread they
 * wake together, re-collide, and synchronize harder each round — which is what
 * keeps a server pinned at its connection cap. A quarter is enough to scatter
 * a burst while leaving the schedule recognisable to someone reading logs.
 */
const backoffJitterFraction = 0.25;

/**
 * How much longer than the server's `Retry-After` a delay may be stretched.
 *
 * `Retry-After` is a floor, not a target: waiting *less* than the server asked
 * is the one thing a client must not do. So this spreads strictly later, never
 * earlier — at `Math.random() === 0` the delay is exactly what was asked for.
 */
const retryAfterStretchFraction = 0.5;

/**
 * Exponential backoff: the first retry waits `base`, each subsequent one
 * doubles, and the doubling stops at `max` — then a random spread of
 * ±`backoffJitterFraction` is applied so that clients which failed at the same
 * instant do not retry at the same instant.
 *
 * The schedule is shared; the values are not. Each stream passes its own
 * `base` and `max` because the policies genuinely differ — the log tail
 * starts fast because a person is waiting, background streams start at the
 * interval the server asks for in its Retry-After.
 *
 * @param attempt 1-based index of the retry about to be made.
 */
export function backoffDelay(attempt: number, { base, max }: BackoffOptions): number {
  const nominal = Math.min(base * 2 ** (Math.max(1, attempt) - 1), max);
  const spread = 1 - backoffJitterFraction + Math.random() * backoffJitterFraction * 2;

  return Math.round(nominal * spread);
}

/**
 * Spreads a server-supplied `Retry-After` so that every client rejected while
 * a cap was full does not wake in the same second and re-collide.
 *
 * The result is never shorter than the server asked for; see
 * `retryAfterStretchFraction`.
 *
 * @param milliseconds the delay the server asked for.
 */
export function retryAfterDelay(milliseconds: number): number {
  return Math.round(milliseconds * (1 + Math.random() * retryAfterStretchFraction));
}
