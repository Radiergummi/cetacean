/**
 * localStorage reads and writes that cannot throw.
 *
 * A browser set to block site data — a private window, enterprise policy, a
 * sandboxed frame — throws a `SecurityError` on the access itself, not on some
 * later call. Every read in this app happens in a `useState` initializer, so an
 * unguarded one takes the render down with it: `ThemeToggle` sits in the app
 * shell, which turned a blocked preference into a blank page.
 *
 * A preference that cannot be stored is not worth reporting to anyone, so both
 * helpers fall back silently and the caller keeps its default.
 */

export function readStoredValue(key: string): string | null {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

export function writeStoredValue(key: string, value: string): void {
  try {
    localStorage.setItem(key, value);
  } catch {
    // Nothing to do — the setting simply does not survive the reload.
  }
}
