import { HighlightedText } from "./LogMessage";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

function highlight(text: string, search: string, useRegex = true) {
  const { container } = render(
    <HighlightedText
      text={text}
      search={search}
      caseSensitive={false}
      useRegex={useRegex}
    />,
  );

  return {
    text: container.textContent,
    marks: [...container.querySelectorAll("mark")].map(({ textContent }) => textContent),
  };
}

describe("HighlightedText", () => {
  it("does not repeat text captured by a group in the pattern", () => {
    const result = highlight("an error here", "(error|warn)");

    expect(result.text).toBe("an error here");
    expect(result.marks).toEqual(["error"]);
  });

  it("survives a group that does not take part in the match", () => {
    const result = highlight("a and b", "(a)|b");

    expect(result.text).toBe("a and b");
    expect(result.marks).toEqual(["a", "a", "b"]);
  });

  it("terminates on a pattern that can match nothing", () => {
    const result = highlight("abc", "x*");

    expect(result.text).toBe("abc");
    expect(result.marks).toEqual([]);
  });

  it("escapes a literal search", () => {
    const result = highlight("a.b axb", "a.b", false);

    expect(result.marks).toEqual(["a.b"]);
  });
});
