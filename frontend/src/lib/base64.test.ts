import { decodeBase64Text, encodeBase64Text } from "./base64";
import { describe, expect, it } from "vitest";

describe("base64 text", () => {
  const text = "Héllo wörld — 日本語 🐳";

  it("round-trips text outside Latin-1", () => {
    expect(decodeBase64Text(encodeBase64Text(text))).toBe(text);
  });

  it("decodes what another encoder produced from UTF-8 bytes", () => {
    expect(decodeBase64Text("SMOpbGxv")).toBe("Héllo");
  });
});
