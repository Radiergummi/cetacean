import { highlightLines } from "./highlight";
import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

function lines(code: string, language: Parameters<typeof highlightLines>[1]) {
  return highlightLines(code, language).map((line) => render(<code>{line}</code>).container);
}

describe("highlightLines", () => {
  it("returns one node per line, empty lines included", () => {
    expect(highlightLines("FROM alpine\n\nRUN true", "dockerfile")).toHaveLength(3);
  });

  it("highlights the shell a RUN instruction carries", () => {
    const [run, continued] = lines('RUN echo "hi" \\\n  && ls $HOME', "dockerfile");

    expect(run?.querySelector(".text-purple-700")?.textContent).toBe("RUN");
    expect(run?.querySelector(".text-green-700")?.textContent).toBe('"hi"');
    expect(continued?.textContent).toBe("  && ls $HOME");
    expect(continued?.querySelector(".text-sky-700")?.textContent).toBe("$HOME");
  });

  it("splits a token that spans lines", () => {
    const [first, second] = lines('key: "a\n  b"', "yaml");

    expect(first?.textContent).toBe('key: "a');
    expect(second?.textContent).toBe('  b"');
  });
});
