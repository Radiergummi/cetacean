import ThemeToggle from "./ThemeToggle";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach } from "vitest";

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

  it("cycles through light → dark → system on click", () => {
    render(<ThemeToggle />);
    // Starts in system mode (no localStorage value)
    expect(screen.getByLabelText("Theme: System")).toBeInTheDocument();

    // system → light
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByLabelText("Theme: Light")).toBeInTheDocument();
    expect(store.get("theme")).toBe("light");

    // light → dark
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByLabelText("Theme: Dark")).toBeInTheDocument();
    expect(document.documentElement.classList.contains("dark")).toBe(true);
    expect(store.get("theme")).toBe("dark");

    // dark → system
    fireEvent.click(screen.getByRole("button"));
    expect(screen.getByLabelText("Theme: System")).toBeInTheDocument();
    expect(store.get("theme")).toBe("system");
  });

  it("reads initial theme from localStorage", () => {
    store.set("theme", "dark");
    render(<ThemeToggle />);
    expect(screen.getByLabelText("Theme: Dark")).toBeInTheDocument();
    expect(document.documentElement.classList.contains("dark")).toBe(true);
  });
});
