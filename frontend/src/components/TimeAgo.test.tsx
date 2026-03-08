import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen } from "@testing-library/react";
import TimeAgo, { timeAgo } from "./TimeAgo";

describe("timeAgo", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns 'just now' for recent dates", () => {
    const now = new Date().toISOString();
    expect(timeAgo(now)).toBe("just now");
  });

  it("returns minutes ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T12:05:00Z"));
    expect(timeAgo("2024-01-01T12:02:00Z")).toBe("3m ago");
    vi.useRealTimers();
  });

  it("returns hours ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T15:00:00Z"));
    expect(timeAgo("2024-01-01T12:00:00Z")).toBe("3h ago");
    vi.useRealTimers();
  });

  it("returns days ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-05T12:00:00Z"));
    expect(timeAgo("2024-01-01T12:00:00Z")).toBe("4d ago");
    vi.useRealTimers();
  });

  it("returns date string for old dates", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-06-01T00:00:00Z"));
    const result = timeAgo("2024-01-01T00:00:00Z");
    // Falls back to toLocaleDateString for > 30 days
    expect(result).not.toContain("ago");
  });

  it("returns 'just now' for future dates", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T12:00:00Z"));
    expect(timeAgo("2024-01-01T12:01:00Z")).toBe("just now");
    vi.useRealTimers();
  });
});

describe("TimeAgo component", () => {
  it("renders a time element", () => {
    const date = new Date().toISOString();
    render(<TimeAgo date={date} />);
    const el = screen.getByText("just now");
    expect(el.tagName).toBe("TIME");
    expect(el).toHaveAttribute("datetime", date);
  });
});
