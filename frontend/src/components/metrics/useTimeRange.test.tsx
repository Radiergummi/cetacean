import { useTimeRange } from "./useTimeRange";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";

function rangeAt(search: string) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <MemoryRouter initialEntries={[`/services/a${search}`]}>{children}</MemoryRouter>
  );

  return renderHook(() => useTimeRange(), { wrapper }).result.current;
}

describe("useTimeRange", () => {
  it("reads a custom window in epoch seconds", () => {
    expect(rangeAt("?from=1700000000&to=1700003600")).toMatchObject({
      customFrom: 1700000000,
      customTo: 1700003600,
    });
  });

  it.each([
    ["unparseable", "?from=abc&to=1"],
    ["empty", "?from=1700000000&to=1700000000"],
    ["reversed", "?from=1700003600&to=1700000000"],
    ["past what a Date holds", "?from=1e300&to=1e301"],
    ["half of one", "?from=1700000000"],
  ])("treats a %s window as no custom range", (_, search) => {
    expect(rangeAt(search)).toMatchObject({ customFrom: null, customTo: null });
  });
});
