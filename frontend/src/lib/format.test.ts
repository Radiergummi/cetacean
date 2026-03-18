import { formatBytes, formatRelativeDate } from "./format";
import { describe, it, expect, vi, afterEach } from "vitest";

describe("formatBytes", () => {
  it("formats bytes", () => {
    expect(formatBytes(500)).toContain("500");
    expect(formatBytes(500)).toContain("B");
  });

  it("formats kilobytes", () => {
    expect(formatBytes(2048)).toContain("2");
    expect(formatBytes(2048)).toMatch(/kB/i);
  });

  it("formats megabytes", () => {
    expect(formatBytes(5 * 1024 * 1024)).toContain("5");
    expect(formatBytes(5 * 1024 * 1024)).toContain("MB");
  });

  it("formats gigabytes", () => {
    const result = formatBytes(2.5 * 1024 * 1024 * 1024);
    expect(result).toContain("GB");
  });

  it("handles zero", () => {
    expect(formatBytes(0)).toContain("0");
    expect(formatBytes(0)).toContain("B");
  });
});

describe("formatRelativeDate", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns 'just now' for recent dates", () => {
    const now = new Date().toISOString();
    expect(formatRelativeDate(now)).toBe("just now");
  });

  it("returns minutes ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T12:05:00Z"));
    expect(formatRelativeDate("2024-01-01T12:02:00Z")).toBe("3m ago");
    vi.useRealTimers();
  });

  it("returns hours ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T15:00:00Z"));
    expect(formatRelativeDate("2024-01-01T12:00:00Z")).toBe("3h ago");
    vi.useRealTimers();
  });

  it("returns days ago", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-05T12:00:00Z"));
    expect(formatRelativeDate("2024-01-01T12:00:00Z")).toBe("4d ago");
    vi.useRealTimers();
  });

  it("returns date string for old dates", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-06-01T00:00:00Z"));
    const result = formatRelativeDate("2024-01-01T00:00:00Z");
    expect(result).not.toContain("ago");
  });

  it("returns 'just now' for future dates", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2024-01-01T12:00:00Z"));
    expect(formatRelativeDate("2024-01-01T12:01:00Z")).toBe("just now");
    vi.useRealTimers();
  });
});
