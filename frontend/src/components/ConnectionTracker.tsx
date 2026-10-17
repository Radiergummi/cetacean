import {
  ConnectionProvider,
  deliver,
  reopenedSync,
  SharedEventsProvider,
  streamListeners,
} from "@/hooks/useResourceStream";
import type { EventsSubscribe, SSEListener } from "@/hooks/useResourceStream";
import { apiPath } from "@/lib/basePath";
import { openEventStream } from "@/lib/eventStream";
import type React from "react";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Holds the app's event streams: one per path, shared by every
 * `useResourceStream` on it. `/events` stays open to report the connection
 * state; any other path opens with its first subscriber and closes after its last.
 */
export function ConnectionTracker({ children }: { children: React.ReactNode }) {
  const [connected, setConnected] = useState(true);
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);

  // Children subscribe in effects that run before this component's own, so the
  // `/events` subscribers are collected before its stream opens.
  const registryRef = useRef(newRegistry());

  const subscribe = useCallback<EventsSubscribe>((path, listener) => {
    const { streams } = registryRef.current;
    let stream = streams.get(path);

    if (!stream) {
      const opened: SharedStream = { listeners: new Set(), close: () => {} };
      opened.close = openShared(path, opened.listeners).close;
      streams.set(path, opened);
      stream = opened;
    }

    const { listeners } = stream;
    listeners.add(listener);

    return () => {
      listeners.delete(listener);

      if (listeners.size === 0 && path !== "/events" && streams.get(path) === stream) {
        stream.close();
        streams.delete(path);
      }
    };
  }, []);

  useEffect(() => {
    const { streams, events } = registryRef.current;
    const touch = () => setLastEventAt(Date.now());

    events.close = openShared("/events", events.listeners, {
      onMessage: touch,
      onOpen: () => setConnected(true),
      onDisconnected: () => setConnected(false),
    }).close;

    return () => {
      for (const stream of streams.values()) {
        stream.close();
      }
    };
  }, []);

  return (
    <ConnectionProvider value={{ connected, lastEventAt }}>
      <SharedEventsProvider value={subscribe}>{children}</SharedEventsProvider>
    </ConnectionProvider>
  );
}

interface SharedStream {
  listeners: Set<SSEListener>;
  close: () => void;
}

function newRegistry() {
  const events: SharedStream = { listeners: new Set(), close: () => {} };

  return { events, streams: new Map([["/events", events]]) };
}

function openShared(
  path: string,
  listeners: Set<SSEListener>,
  options: {
    onMessage?: (() => void) | undefined;
    onOpen?: (() => void) | undefined;
    onDisconnected?: (() => void) | undefined;
  } = {},
) {
  const { onMessage, ...connection } = options;
  const handlers = streamListeners((event) => deliver(listeners, event));

  return openEventStream(apiPath(path), {
    // Touched once per message, not once per event a batch carries.
    listeners: Object.fromEntries(
      Object.entries(handlers).map(([type, handle]) => [
        type,
        (message: MessageEvent) => {
          onMessage?.();
          handle(message);
        },
      ]),
    ),
    onReopened: () => deliver(listeners, reopenedSync),
    ...connection,
  });
}
