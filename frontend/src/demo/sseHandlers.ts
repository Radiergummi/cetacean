import type { Dataset } from "./dataset";
import { handleInstantQuery, handleRangeQuery } from "./prometheus";
import { sse } from "msw";

/**
 * Minimal client interface matching what MSW's sse() provides.
 * We only need `send` for broadcasting events.
 */
interface SSEClient {
  send(payload: { data: string; event?: string; id?: string }): void;
}

export interface SSEClients {
  /** /events subscribers */
  global: Set<SSEClient>;
  /** /nodes, /services, etc. subscribers */
  byType: Map<string, Set<SSEClient>>;
  /** /nodes/{id}, /services/{id}, etc. subscribers */
  byId: Map<string, Set<SSEClient>>;
}

const resourceTypes = [
  "nodes",
  "services",
  "tasks",
  "configs",
  "secrets",
  "networks",
  "volumes",
  "stacks",
] as const;

const detailTypes = [
  "nodes",
  "services",
  "tasks",
  "configs",
  "secrets",
  "networks",
  "volumes",
] as const;

export function createSSEHandlers(dataset: Dataset) {
  const clients: SSEClients = {
    global: new Set(),
    byType: new Map(),
    byId: new Map(),
  };

  const handlers = [
    // Global events
    sse("*/events", ({ client }) => {
      clients.global.add(client as unknown as SSEClient);
    }),

    // Per-type SSE (list pages)
    ...resourceTypes.map((type) =>
      sse(`*/${type}`, ({ client }) => {
        if (!clients.byType.has(type)) {
          clients.byType.set(type, new Set());
        }

        clients.byType.get(type)!.add(client as unknown as SSEClient);
      }),
    ),

    // Per-resource SSE (detail pages)
    ...detailTypes.map((type) =>
      sse(`*/${type}/:id`, ({ client, params }) => {
        const key = `${type}/${params.id}`;

        if (!clients.byId.has(key)) {
          clients.byId.set(key, new Set());
        }

        clients.byId.get(key)!.add(client as unknown as SSEClient);
      }),
    ),

    // Metrics SSE stream (live chart data)
    sse("*/metrics", ({ client, request }) => {
      const sseClient = client as unknown as SSEClient;
      const url = new URL(request.url);
      const query = url.searchParams.get("query") ?? "";
      const rangeSec = parseInt(url.searchParams.get("range") ?? "3600", 10);
      const step = parseInt(url.searchParams.get("step") ?? "15", 10);

      const now = Date.now() / 1000;
      const start = now - rangeSec;
      const initialData = handleRangeQuery(query, start, now, step, dataset);

      // Send full range as the initial event.
      sseClient.send({
        event: "initial",
        data: JSON.stringify({ data: initialData }),
      });

      // Send periodic point events every 15 seconds.
      // Track the interval so we can clear it on send failure.
      let stopped = false;
      const interval = setInterval(() => {
        if (stopped) {
          return;
        }

        try {
          const pointData = handleInstantQuery(query, dataset);
          sseClient.send({
            event: "point",
            data: JSON.stringify({ data: pointData }),
          });
        } catch {
          stopped = true;
          clearInterval(interval);
        }
      }, 15_000);
    }),
  ];

  return { handlers, clients };
}

/**
 * Broadcast an SSE event to all relevant subscribers.
 * Silently removes clients that fail to receive (disconnected).
 */
export function broadcast(
  clients: SSEClients,
  eventType: string,
  typePlural: string,
  resourceId: string,
  data: Record<string, unknown>,
) {
  const payload = JSON.stringify(data);
  const message = { event: eventType, data: payload };

  const send = (client: SSEClient, set: Set<SSEClient>) => {
    try {
      client.send(message);
    } catch {
      set.delete(client);
    }
  };

  // Global /events subscribers
  for (const client of clients.global) {
    send(client, clients.global);
  }

  // Per-type list subscribers (e.g. /services)
  const typeSet = clients.byType.get(typePlural);

  if (typeSet) {
    for (const client of typeSet) {
      send(client, typeSet);
    }
  }

  // Per-resource detail subscribers (e.g. /services/{id})
  const idKey = `${typePlural}/${resourceId}`;
  const idSet = clients.byId.get(idKey);

  if (idSet) {
    for (const client of idSet) {
      send(client, idSet);
    }
  }
}
