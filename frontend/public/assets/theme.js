/**
 * Applies the stored theme before the first paint.
 *
 * This has to run render-blocking in <head>: the React tree sets the same
 * class from an effect, which lands after the browser has already painted a
 * light background, so a dark-mode reader saw a white flash on every load.
 *
 * It is a file rather than an inline script because the server's
 * Content-Security-Policy has no `script-src`, so it falls back to
 * `default-src 'self'` and inline scripts are blocked.
 *
 * The resolution rule is mirrored in components/ThemeToggle.tsx, which owns
 * the theme once React is running. Keep the two in step.
 */
try {
  var stored = localStorage.getItem("theme");
  var theme = stored === "dark" || stored === "light" || stored === "system" ? stored : "system";
  var dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);

  document.documentElement.classList.toggle("dark", dark);
} catch {
  // A browser set to block site data throws on localStorage. The default
  // stylesheet is the light theme, which is what the page already shows.
}
