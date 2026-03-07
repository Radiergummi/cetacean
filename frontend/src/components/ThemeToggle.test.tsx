import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ThemeToggle from "./ThemeToggle";

const store = new Map<string, string>();

beforeEach(() => {
  store.clear();
  vi.stubGlobal("localStorage", {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => store.set(key, value),
    removeItem: (key: string) => store.delete(key),
  });
  document.documentElement.classList.remove("dark");
  // Default to light mode in matchMedia
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: false,
    media: query,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
});

describe("ThemeToggle", () => {
  it("renders toggle button", () => {
    render(<ThemeToggle />);
    expect(screen.getByRole("button")).toBeInTheDocument();
  });

  it("toggles theme on click", () => {
    render(<ThemeToggle />);
    // Starts in light mode
    expect(screen.getByTitle("Switch to dark mode")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button"));
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(store.get("theme")).toBe("dark");
  });

  it("reads initial theme from localStorage", () => {
    store.set("theme", "dark");
    render(<ThemeToggle />);
    expect(screen.getByTitle("Switch to light mode")).toBeInTheDocument();
  });
});
