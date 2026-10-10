import type { Dataset } from "./dataset";
import { createHandlers } from "./handlers";
import { createSSEHandlers } from "./sseHandlers";

/**
 * Build the demo's request handlers. The SSE handlers come first: they match
 * only `Accept: text/event-stream`, while the JSON handlers on the same paths
 * match any request and would otherwise answer every stream.
 */
export function createDemoHandlers(dataset: Dataset) {
  const { handlers: sseHandlers, clients } = createSSEHandlers(dataset);
  const httpHandlers = createHandlers(dataset, clients);

  return { handlers: [...sseHandlers, ...httpHandlers], clients };
}
