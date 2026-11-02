/**
 * Whether the browser runs on macOS, where Cmd plays the part Ctrl plays elsewhere.
 */
export const isMac = typeof navigator !== "undefined" && /Mac/.test(navigator.platform ?? "");

/**
 * The submit shortcut as a button shows it, in front of its label.
 */
export const submitShortcutLabel = isMac ? "⌘⏎" : "Ctrl⏎";
