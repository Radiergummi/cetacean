import { staleWriteMessage } from "@/api/client";
import { EnvEditor } from "@/components/service-detail/EnvEditor";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mockFetch = vi.fn<(path: string, init?: RequestInit) => Promise<Response>>();

beforeEach(() => {
  vi.stubGlobal("fetch", mockFetch);
  mockFetch.mockReset();
});

function respond(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(status === 204 ? null : JSON.stringify(body), { status, headers });
}

async function editAndSave() {
  render(
    <EnvEditor
      serviceId="svc1"
      envVars={{ MODE: "a" }}
      onSaved={() => {}}
      canEdit
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );

  fireEvent.click(screen.getByRole("button", { name: /edit/i }));
  fireEvent.change(screen.getByDisplayValue("a"), { target: { value: "b" } });
  fireEvent.click(screen.getByRole("button", { name: /save/i }));
}

describe("EnvEditor", () => {
  it("saves against the version it opened on", async () => {
    mockFetch.mockImplementation(async (_path, init) =>
      init?.method === "HEAD"
        ? respond(200, null, { ETag: '"v1"' })
        : respond(200, { env: { MODE: "b" } }),
    );

    await editAndSave();

    await waitFor(() => {
      const patch = mockFetch.mock.calls.find(([, init]) => init?.method === "PATCH");
      expect(new Headers(patch?.[1]?.headers).get("If-Match")).toBe('"v1"');
    });
  });

  it("says to reload when someone else changed it first", async () => {
    mockFetch.mockImplementation(async (_path, init) =>
      init?.method === "HEAD"
        ? respond(200, null, { ETag: '"v1"' })
        : respond(412, { title: "Precondition Failed" }),
    );

    await editAndSave();

    expect(await screen.findByText(staleWriteMessage)).toBeInTheDocument();
  });
});
