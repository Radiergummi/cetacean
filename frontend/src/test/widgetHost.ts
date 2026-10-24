import { act } from "@testing-library/react";
import { useState } from "react";
import { vi } from "vitest";

type Listener = (event: { arguments?: Record<string, unknown> }) => void;

/**
 * A stand-in for the MCP Apps host a widget talks to. Tool calls never settle
 * unless a test says otherwise, so a widget stays on its loading message.
 */
function createFakeApp() {
  const listeners = new Map<string, Listener>();

  return {
    listeners,
    addEventListener: (type: string, listener: Listener) => listeners.set(type, listener),
    callServerTool: vi.fn<
      (request: { name: string; arguments?: Record<string, unknown> }) => Promise<unknown>
    >(() => new Promise(() => {})),
    sendMessage: vi.fn<() => Promise<unknown>>(),
  };
}

export let fakeApp = createFakeApp();

/** Starts each test with a host that has not sent the tool input yet. */
export function resetFakeHost() {
  fakeApp = createFakeApp();
}

/** Delivers the arguments of the tool call the widget renders for. */
export function sendToolInput(args: Record<string, unknown>) {
  act(() => {
    fakeApp.listeners.get("toolinput")?.({ arguments: args });
  });
}

/**
 * Replaces `@modelcontextprotocol/ext-apps/react`: one app per mount, created
 * already connected, with `onAppCreated` run before anything else as the SDK does.
 */
export const extAppsMock = {
  useApp: ({ onAppCreated }: { onAppCreated?: (app: typeof fakeApp) => void }) => {
    const [app] = useState(() => {
      onAppCreated?.(fakeApp);

      return fakeApp;
    });

    return { app, isConnected: true, error: null };
  },
  useHostStyleVariables: () => {},
};
