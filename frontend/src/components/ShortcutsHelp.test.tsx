import { navigationShortcuts } from "../lib/shortcuts";
import ShortcutsHelp from "./ShortcutsHelp";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

describe("ShortcutsHelp", () => {
  it("lists every navigation chord that is registered", () => {
    render(<ShortcutsHelp onClose={vi.fn<() => void>()} />);

    for (const { description } of navigationShortcuts) {
      expect(
        screen.getByText(description),
        `the ? overlay omits "${description}"`,
      ).toBeInTheDocument();
    }
  });

  it("shows the metrics and recommendations chords", () => {
    render(<ShortcutsHelp onClose={vi.fn<() => void>()} />);

    expect(screen.getByText("Go to metrics")).toBeInTheDocument();
    expect(screen.getByText("Go to recommendations")).toBeInTheDocument();
  });
});

describe("navigationShortcuts", () => {
  it("gives every chord a distinct key sequence", () => {
    const sequences = navigationShortcuts.map(({ keys }) => keys.join(" "));

    expect(new Set(sequences).size).toBe(sequences.length);
  });

  it("starts every chord with g, which useHotkeys treats as the prefix", () => {
    for (const { keys } of navigationShortcuts) {
      expect(keys).toHaveLength(2);
      expect(keys[0]).toBe("g");
    }
  });
});
