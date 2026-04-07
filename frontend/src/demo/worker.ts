import { buildDataset } from "./dataset";
import { createHandlers } from "./handlers";
import { startSimulator } from "./simulator";
import { createSSEHandlers } from "./sseHandlers";
import { setupWorker } from "msw/browser";

const dataset = buildDataset();
const { handlers: sseHandlers, clients } = createSSEHandlers(dataset);
const httpHandlers = createHandlers(dataset, clients);

export const worker = setupWorker(...httpHandlers, ...sseHandlers);
export { dataset, clients };

/**
 * Start the worker and simulator.
 * Call this once from the demo entry point.
 */
export async function startDemo() {
  await worker.start({
    onUnhandledRequest: "bypass",
    serviceWorker: { url: "/demo/mockServiceWorker.js" },
  });
  startSimulator(dataset, clients);
}
