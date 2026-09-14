import ComposeSection from "./ComposeSection";
import { createTestQueryClient, createWrapper } from "@/test/mocks";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const document = `# Exported from Cetacean.
services:
  api:
    image: nginx:1.27
`;

beforeEach(() => {
  vi.restoreAllMocks();
});

function renderSection(fetcher: (signal?: AbortSignal) => Promise<string>) {
  return render(
    <ComposeSection
      name="web"
      queryKey="stack:web"
      fetcher={fetcher}
    />,
    { wrapper: createWrapper(createTestQueryClient()) },
  );
}

describe("ComposeSection", () => {
  // Most visits to a detail page do not want the export, so rendering one must
  // not cost a request.
  it("fetches nothing until it is expanded", () => {
    const fetcher = vi.fn<() => Promise<string>>().mockResolvedValue(document);

    renderSection(fetcher);

    expect(fetcher).not.toHaveBeenCalled();
  });

  it("renders the document once expanded", async () => {
    const fetcher = vi.fn<() => Promise<string>>().mockResolvedValue(document);

    renderSection(fetcher);
    await userEvent.click(screen.getByRole("button", { name: /compose file/i }));

    await waitFor(() => {
      expect(fetcher).toHaveBeenCalled();
    });
    expect(await screen.findByText(/Exported from Cetacean/)).toBeInTheDocument();
  });

  it("reports a failure instead of rendering an empty document", async () => {
    renderSection(vi.fn<() => Promise<string>>().mockRejectedValue(new Error("stack not found")));

    await userEvent.click(screen.getByRole("button", { name: /compose file/i }));

    expect(await screen.findByText("stack not found")).toBeInTheDocument();
  });
});
