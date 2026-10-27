/** FNV-1a: cheap and stable, which is all a bucket assignment needs. */
function hash(value: string): number {
  let result = 0x811c9dc5;

  for (let index = 0; index < value.length; index++) {
    result ^= value.charCodeAt(index);
    result = Math.imul(result, 0x01000193);
  }

  return result >>> 0;
}

/**
 * Splits IDs into about `size`-sized batches by a hash of each ID, so an ID
 * joining or leaving changes only its own batch. The bucket count is rounded
 * up to a power of two, so it changes only when the IDs double or halve.
 */
export function stableBatches(ids: readonly string[], size: number): string[][] {
  if (ids.length === 0) {
    return [];
  }

  const count = 2 ** Math.ceil(Math.log2(Math.ceil(ids.length / size)));
  const buckets: string[][] = Array.from({ length: count }, () => []);

  for (const id of [...ids].sort()) {
    buckets[hash(id) % count]?.push(id);
  }

  return buckets.filter((bucket) => bucket.length > 0);
}
