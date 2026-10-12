import { Layout } from "./App";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, useNavigate } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

vi.mock("./components/ConnectionStatus", () => ({ default: () => null }));
vi.mock("./components/AtomFeedLink", () => ({ default: () => null }));
vi.mock("./components/Footer", () => ({ default: () => null }));
vi.mock("./components/ThemeToggle", () => ({ default: () => null }));
vi.mock("./components/search", () => ({ GlobalSearch: () => null }));
vi.mock("./hooks/useAuth", () => ({ useAuth: () => ({ identity: null, loading: false }) }));
vi.mock("./hooks/useRecommendations", () => ({ useRecommendations: () => ({ total: 0 }) }));

function Back() {
  const navigate = useNavigate();

  return (
    <button
      type="button"
      onClick={() => navigate(-1)}
    >
      Back
    </button>
  );
}

describe("Layout", () => {
  it("closes the mobile menu on back navigation", async () => {
    const user = userEvent.setup();
    render(
      <MemoryRouter
        initialEntries={["/nodes", "/services"]}
        initialIndex={1}
      >
        <Layout>
          <Back />
        </Layout>
      </MemoryRouter>,
    );

    await user.click(screen.getByRole("button", { name: "Toggle menu" }));
    expect(screen.getAllByRole("link", { name: "Nodes" })).toHaveLength(2);

    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getAllByRole("link", { name: "Nodes" })).toHaveLength(1);
  });
});
