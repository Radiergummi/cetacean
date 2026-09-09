import { readStoredValue, writeStoredValue } from "@/lib/storage";
import { Monitor, Moon, Sun } from "lucide-react";
import { useEffect, useState } from "react";

type Theme = "light" | "dark" | "system";

const cycle: Theme[] = ["light", "dark", "system"];

/**
 * The same resolution the inline script in index.html runs before the first
 * paint. That script owns the initial class so the page never flashes the
 * wrong theme; this component owns it from the first render onwards. Keep the
 * two in step — the server hashes that script's bytes for the CSP, so editing
 * it changes the header too.
 */
function getInitialTheme(): Theme {
  const stored = readStoredValue("theme");

  if (stored === "dark" || stored === "light" || stored === "system") {
    return stored;
  }

  return "system";
}

function resolveTheme(theme: Theme): "light" | "dark" {
  if (theme !== "system") {
    return theme;
  }

  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const icons: Record<Theme, typeof Sun> = { light: Sun, dark: Moon, system: Monitor };
const labels: Record<Theme, string> = { light: "Light", dark: "Dark", system: "System" };

export default function ThemeToggle() {
  const [theme, setTheme] = useState(getInitialTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolveTheme(theme) === "dark");
    writeStoredValue("theme", theme);
  }, [theme]);

  // Listen for OS theme changes when in system mode
  useEffect(() => {
    if (theme !== "system") {
      return;
    }

    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => document.documentElement.classList.toggle("dark", mediaQuery.matches);

    mediaQuery.addEventListener("change", handler);

    return () => mediaQuery.removeEventListener("change", handler);
  }, [theme]);

  const Icon = icons[theme];

  return (
    <button
      type="button"
      onClick={() => setTheme(cycle[(cycle.indexOf(theme) + 1) % cycle.length] ?? "system")}
      aria-label={`Theme: ${labels[theme]}`}
      className="flex size-8 cursor-pointer items-center justify-center rounded-md transition hover:bg-muted"
    >
      <Icon className="size-4" />
    </button>
  );
}
