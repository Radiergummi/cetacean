import PerResource from "./PerResource";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { Link, MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it } from "vitest";

function Draft() {
  const [draft, setDraft] = useState("");

  return (
    <>
      <input
        aria-label="draft"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
      />
      <Link to="/services/b">next</Link>
    </>
  );
}

describe("PerResource", () => {
  it("drops the last resource's state when the route moves to another", () => {
    render(
      <MemoryRouter initialEntries={["/services/a"]}>
        <Routes>
          <Route
            path="/services/:id"
            element={
              <PerResource>
                <Draft />
              </PerResource>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    fireEvent.change(screen.getByLabelText("draft"), { target: { value: "A's edit" } });
    fireEvent.click(screen.getByText("next"));

    expect(screen.getByLabelText("draft")).toHaveValue("");
  });
});
