import { showErrorToast } from "./showErrorToast";
import { ApiError } from "@/api/client";
import { toast } from "sonner";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("sonner", () => ({ toast: { error: vi.fn<() => void>() } }));

beforeEach(() => {
  vi.mocked(toast.error).mockReset();
});

describe("showErrorToast", () => {
  it("keeps the server's detail for a known code, with the suggestion after it", () => {
    showErrorToast(
      new ApiError("/api/errors/NOD001", "Node Not Down", 409, "node worker-1 is still ready"),
      "Failed",
    );

    const [title, options] = vi.mocked(toast.error).mock.calls[0] ?? [];
    const description = String((options as { description?: unknown } | undefined)?.description);

    expect(title).toBe("Node not down");
    expect(description).toMatch(/^node worker-1 is still ready\. Demote the node/);
  });

  it("falls back to the suggestion when the server gave no detail", () => {
    showErrorToast(new ApiError("/api/errors/NOD001", "Node Not Down", 409, ""), "Failed");

    const [, options] = vi.mocked(toast.error).mock.calls[0] ?? [];

    expect((options as { description?: unknown } | undefined)?.description).toMatch(/^Demote/);
  });

  it("shows the message of an unknown error", () => {
    showErrorToast(new Error("boom"), "Failed");

    expect(vi.mocked(toast.error)).toHaveBeenCalledWith("boom");
  });
});
