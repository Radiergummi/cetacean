import { linkTarget } from "./linkHeader";
import { describe, expect, it } from "vitest";

describe("linkTarget", () => {
  it("finds the next link among several", () => {
    const header = '</history?limit=2&before=4>; rel="next", </history?limit=2>; rel="first"';

    expect(linkTarget(header, "next")).toBe("/history?limit=2&before=4");
    expect(linkTarget(header, "first")).toBe("/history?limit=2");
  });

  it("is undefined when no link has the relation", () => {
    expect(linkTarget('</nodes?offset=0>; rel="prev"', "next")).toBeUndefined();
    expect(linkTarget(null, "next")).toBeUndefined();
    expect(linkTarget("", "next")).toBeUndefined();
  });

  it("matches one of several space-separated relation types, ignoring case", () => {
    expect(linkTarget('</a>; rel="previous NEXT"', "next")).toBe("/a");
  });

  it("accepts an unquoted rel and a parameter name in any case", () => {
    expect(linkTarget("</a>;REL=next", "next")).toBe("/a");
  });

  it("does not split a link on a comma or semicolon inside a quoted value", () => {
    const header = '</a>; title="one, two; three"; rel="prev", </b>; rel="next"';

    expect(linkTarget(header, "next")).toBe("/b");
    expect(linkTarget(header, "prev")).toBe("/a");
  });

  it("keeps the first rel when a link repeats it", () => {
    expect(linkTarget('</a>; rel="prev"; rel="next"', "next")).toBeUndefined();
  });
});
