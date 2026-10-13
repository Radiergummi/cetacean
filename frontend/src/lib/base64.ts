/**
 * Encode text as base64 of its UTF-8 bytes. `btoa` takes Latin-1 only and
 * throws on anything else.
 */
export function encodeBase64Text(text: string): string {
  let binary = "";

  for (const byte of new TextEncoder().encode(text)) {
    binary += String.fromCharCode(byte);
  }

  return btoa(binary);
}

/**
 * Decode base64 into text, reading the bytes as UTF-8. `atob` alone yields one
 * character per byte, which garbles anything outside ASCII.
 */
export function decodeBase64Text(data: string): string {
  return new TextDecoder().decode(
    Uint8Array.from(atob(data), (character) => character.charCodeAt(0)),
  );
}
