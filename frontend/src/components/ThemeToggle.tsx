import { Monitor, Moon, Sun } from "lucide-react";
import { useState, useEffect } from "react";

type Theme = "light" | "dark" | "system";

const CYCLE: Theme[] = ["light", "dark", "system"];

function getInitialTheme(): Theme {
  if (typeof window === "undefined") return "system";
  const stored = localStorage.getItem("theme");
  if (stored === "dark" || stored === "light" || stored === "system") return stored;
  return "system";
}

function resolveTheme(theme: Theme): "light" | "dark" {
  if (theme !== "system") return theme;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

const icons: Record<Theme, typeof Sun> = { light: Sun, dark: Moon, system: Monitor };
const labels: Record<Theme, string> = { light: "Light", dark: "Dark", system: "System" };

export default function ThemeToggle() {
  const [theme, setTheme] = useState(getInitialTheme);

  useEffect(() => {
    document.documentElement.classList.toggle("dark", resolveTheme(theme) === "dark");
    localStorage.setItem("theme", theme);
  }, [theme]);

  // Listen for OS theme changes when in system mode
  useEffect(() => {
    if (theme !== "system") return;
    const mq = window.matchMedia("(prefers-color-scheme: dark)");
    const handler = () => document.documentElement.classList.toggle("dark", mq.matches);
    mq.addEventListener("change", handler);
    return () => mq.removeEventListener("change", handler);
  }, [theme]);

  const Icon = icons[theme];

  return (
    <button
      onClick={() => setTheme(CYCLE[(CYCLE.indexOf(theme) + 1) % CYCLE.length])}
      className="flex size-8 cursor-pointer items-center justify-center rounded-md transition hover:bg-muted"
      title={`Theme: ${labels[theme]}`}
    >
      <Icon className="size-4" />
    </button>
  );
}
