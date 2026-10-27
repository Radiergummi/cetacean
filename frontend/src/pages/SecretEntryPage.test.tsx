import SecretEntryPage from "./SecretEntryPage";
import { ApiError, api } from "@/api/client";
import type { SecretDetail, SecretEntry } from "@/api/types";
import { createTestQueryClient } from "@/test/mocks";
import { QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/api/client", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/api/client")>();

  return {
    ApiError: original.ApiError,
    api: {
      secretEntry: vi.fn<(id: string) => Promise<SecretEntry>>(),
      completeSecretEntry: vi.fn<(id: string, data: string) => Promise<SecretDetail>>(),
    },
  };
});

const entry: SecretEntry = {
  id: "ENTRY1",
  name: "db_password",
  labels: { team: "shop" },
  encoding: "utf8",
  expiresAt: "2026-10-25T12:00:00Z",
};

function renderAt(path: string) {
  return render(
    <QueryClientProvider client={createTestQueryClient()}>
      <MemoryRouter initialEntries={[path]}>
        <SecretEntryPage />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("SecretEntryPage", () => {
  beforeEach(() => {
    vi.mocked(api.secretEntry).mockReset();
    vi.mocked(api.completeSecretEntry).mockReset();
  });

  it("prefills from the server's record, not the URL", async () => {
    vi.mocked(api.secretEntry).mockResolvedValue(entry);

    renderAt("/secrets/new?elicitation=ENTRY1&name=evil");

    await waitFor(() => expect(screen.getByText("New secret db_password")).toBeInTheDocument());

    expect(api.secretEntry).toHaveBeenCalledWith("ENTRY1", expect.anything());
    expect(screen.getByText("team=shop")).toBeInTheDocument();
    expect(screen.queryByText(/evil/)).not.toBeInTheDocument();
  });

  it("sends the value base64-encoded with the elicitation ID", async () => {
    vi.mocked(api.secretEntry).mockResolvedValue(entry);
    vi.mocked(api.completeSecretEntry).mockResolvedValue({
      secret: { ID: "sec1" } as SecretDetail["secret"],
      services: [],
      usedBy: 0,
    });

    renderAt("/secrets/new?elicitation=ENTRY1");

    const field = await screen.findByLabelText("Value");
    fireEvent.change(field, { target: { value: "hunter2" } });
    fireEvent.click(screen.getByRole("button", { name: "Create secret" }));

    await waitFor(() => expect(screen.getByText("Secret db_password created")).toBeInTheDocument());

    expect(api.completeSecretEntry).toHaveBeenCalledWith("ENTRY1", btoa("hunter2"));
  });

  it("passes a base64 value through as typed", async () => {
    vi.mocked(api.secretEntry).mockResolvedValue({ ...entry, encoding: "base64" });
    vi.mocked(api.completeSecretEntry).mockResolvedValue({
      secret: { ID: "sec1" } as SecretDetail["secret"],
      services: [],
      usedBy: 0,
    });

    renderAt("/secrets/new?elicitation=ENTRY1");

    const field = await screen.findByLabelText("Value, base64-encoded");
    fireEvent.change(field, { target: { value: "aGVsbG8=" } });
    fireEvent.click(screen.getByRole("button", { name: "Create secret" }));

    await waitFor(() => expect(api.completeSecretEntry).toHaveBeenCalledWith("ENTRY1", "aGVsbG8="));
  });

  it("says when someone else's agent opened the request", async () => {
    vi.mocked(api.secretEntry).mockRejectedValue(
      new ApiError("/api/errors/SEC007", "Secret Entry Opened By Someone Else", 403, ""),
    );

    renderAt("/secrets/new?elicitation=ENTRY1");

    await waitFor(() =>
      expect(screen.getByText(/opened by someone else's agent/)).toBeInTheDocument(),
    );
  });

  it("asks for the agent's link without an elicitation", () => {
    renderAt("/secrets/new");

    expect(screen.getByText(/link your agent gave you/)).toBeInTheDocument();
    expect(api.secretEntry).not.toHaveBeenCalled();
  });
});
