import { buildDataset } from "./dataset";
import { createDemoHandlers } from "./demoHandlers";
import { getResponse } from "msw";
import { describe, expect, it, vi } from "vitest";

// msw refuses to build an SSE handler where EventSource does not exist.
vi.stubGlobal("EventSource", class {});

describe("createDemoHandlers", () => {
  const { handlers } = createDemoHandlers(buildDataset());

  it("answers an event stream request with a stream", async () => {
    const request = new Request("http://localhost/nodes", {
      headers: { Accept: "text/event-stream" },
    });
    const response = await getResponse(handlers, request);

    expect(response?.headers.get("content-type")).toBe("text/event-stream");
  });

  it("still answers a JSON request with JSON", async () => {
    const request = new Request("http://localhost/nodes", {
      headers: { Accept: "application/json" },
    });
    const response = await getResponse(handlers, request);

    expect(response?.headers.get("content-type")).toContain("json");
  });
});
