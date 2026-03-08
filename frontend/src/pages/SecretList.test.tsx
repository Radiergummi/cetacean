import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { SSEProvider } from "../hooks/SSEContext";
import SecretList from "./SecretList";
import type { Secret } from "../api/types";

class MockEventSource {
  static instance: MockEventSource;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  listeners = new Map<string, ((e: MessageEvent) => void)[]>();
  closed = false;
  constructor(_url: string) {
    MockEventSource.instance = this;
  }
  addEventListener(type: string, handler: (e: MessageEvent) => void) {
    const existing = this.listeners.get(type) || [];
    existing.push(handler);
    this.listeners.set(type, existing);
  }
  close() {
    this.closed = true;
  }
}

vi.mock("../api/client", () => ({
  api: {
    secrets: vi.fn(),
  },
}));

import { api } from "../api/client";
const mockSecrets = vi.mocked(api.secrets);

const fakeSecret = (id: string, name: string): Secret => ({
  ID: id,
  Version: { Index: 1 },
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-02T00:00:00Z",
  Spec: { Name: name, Labels: {} },
});

beforeEach(() => {
  vi.stubGlobal("EventSource", MockEventSource);
  vi.stubGlobal("localStorage", {
    getItem: () => null,
    setItem: vi.fn(),
    removeItem: vi.fn(),
  });
  mockSecrets.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function wrapper({ children }: { children: ReactNode }) {
  return (
    <MemoryRouter>
      <SSEProvider>{children}</SSEProvider>
    </MemoryRouter>
  );
}

describe("SecretList", () => {
  it("renders secret list", async () => {
    mockSecrets.mockResolvedValue({
      items: [fakeSecret("s1", "db-password"), fakeSecret("s2", "api-key")],
      total: 2,
    });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("db-password")).toBeInTheDocument();
    });
    expect(screen.getByText("api-key")).toBeInTheDocument();
  });

  it("shows empty state", async () => {
    mockSecrets.mockResolvedValue({ items: [], total: 0 });
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("No secrets found")).toBeInTheDocument();
    });
  });

  it("shows error state", async () => {
    mockSecrets.mockRejectedValue(new Error("Forbidden"));
    render(<SecretList />, { wrapper });

    await waitFor(() => {
      expect(screen.getByText("Forbidden")).toBeInTheDocument();
    });
  });
});
