import {
  ConnectionProvider,
  reopenedSync,
  SharedEventsProvider,
  streamListeners,
} from "@/hooks/useResourceStream";
import type { EventsSubscribe, SSEEvent, SSEListener } from "@/hooks/useResourceStream";
import { apiPath } from "@/lib/basePath";
import { openEventStream } from "@/lib/eventStream";
import type React from "react";
import { useCallback, useEffect, useRef, useState } from "react";

/**
 * Holds the app's one `/events` stream: it reports the connection state, and
 * shares the stream's events with every `useResourceStream("/events")`.
 */
export function ConnectionTracker({ children }: { children: React.ReactNode }) {
  const [connected, setConnected] = useState(true);
  const [lastEventAt, setLastEventAt] = useState<number | null>(null);
  const listenersRef = useRef(new Set<SSEListener>());

  const subscribe = useCallback<EventsSubscribe>((listener) => {
    const listeners = listenersRef.current;
    listeners.add(listener);

    return () => {
      listeners.delete(listener);
    };
  }, []);

  useEffect(() => {
    const dispatch = (event: SSEEvent) => {
      for (const listener of listenersRef.current) {
        listener(event);
      }
    };

    const listeners = streamListeners((event) => {
      setLastEventAt(Date.now());
      dispatch(event);
    });

    const stream = openEventStream(apiPath("/events"), {
      listeners,
      onOpen: () => setConnected(true),
      onDisconnected: () => setConnected(false),
      onReopened: () => dispatch(reopenedSync),
    });

    return () => stream.close();
  }, []);

  return (
    <ConnectionProvider value={{ connected, lastEventAt }}>
      <SharedEventsProvider value={subscribe}>{children}</SharedEventsProvider>
    </ConnectionProvider>
  );
}
