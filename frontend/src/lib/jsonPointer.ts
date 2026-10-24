/**
 * The JSON Pointer (RFC 6901) naming one key of a flat map. `~` is escaped
 * before `/`, so a key containing `~1` survives the server's decoding intact.
 */
export function pointerTo(key: string): string {
  return `/${key.replaceAll("~", "~0").replaceAll("/", "~1")}`;
}
