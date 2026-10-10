import { QueryInput } from "./QueryInput";
import type { QueryCompletion } from "./useQueryCompletion";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

const completion: QueryCompletion = {
  suggestions: [{ label: "rate", type: "function" }],
  loading: false,
  complete: vi.fn<QueryCompletion["complete"]>(),
  clear: vi.fn<QueryCompletion["clear"]>(),
};

describe("QueryInput", () => {
  // ARIA in HTML allows no role on a textarea; a textbox may still own a listbox.
  it("keeps the textarea a textbox that points at the highlighted suggestion", () => {
    render(
      <QueryInput
        value="ra"
        onChange={vi.fn<(value: string) => void>()}
        onRun={vi.fn<() => void>()}
        loading={false}
        completion={completion}
      />,
    );

    const input = screen.getByRole("textbox");

    expect(input).not.toHaveAttribute("role");
    expect(input).not.toHaveAttribute("aria-expanded");
    expect(input).toHaveAttribute("aria-autocomplete", "list");
    expect(input).toHaveAttribute("aria-controls", "query-suggestions");
    expect(input).toHaveAttribute("aria-activedescendant", "query-suggestion-0");
    expect(screen.getByRole("option", { name: /rate/ })).toHaveAttribute(
      "id",
      "query-suggestion-0",
    );
  });
});
