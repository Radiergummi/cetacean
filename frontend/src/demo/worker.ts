import { buildDataset } from "./dataset";
import { createDemoHandlers } from "./demoHandlers";
import { startSimulator } from "./simulator";
import { setupWorker } from "msw/browser";

const dataset = buildDataset();
const { handlers, clients } = createDemoHandlers(dataset);

export const worker = setupWorker(...handlers);
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
