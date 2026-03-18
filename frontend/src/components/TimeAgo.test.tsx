import TimeAgo from "./TimeAgo";
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("TimeAgo component", () => {
  it("renders a time element", () => {
    const date = new Date().toISOString();
    render(<TimeAgo date={date} />);
    const el = screen.getByText("just now");
    expect(el.tagName).toBe("TIME");
    expect(el).toHaveAttribute("datetime", date);
  });
});
