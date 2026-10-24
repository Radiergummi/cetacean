import { EncryptionPanel } from "./EncryptionPanel";
import { buildDataset } from "@/demo/dataset";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

function renderPanel(editable: string[]) {
  const { Spec } = buildDataset().swarm;
  const spec = { ...Spec, EncryptionConfig: { AutoLockManagers: true } };

  render(
    <EncryptionPanel
      spec={spec}
      editable={new Set(editable)}
      onSaved={() => {}}
    />,
  );
}

describe("EncryptionPanel", () => {
  it("offers no unlock key actions to a caller the server linked none for", () => {
    renderPanel([]);

    expect(screen.queryByText("Show Unlock Key")).not.toBeInTheDocument();
    expect(screen.queryByText("Rotate Unlock Key")).not.toBeInTheDocument();
    expect(screen.queryByText("Unlock Swarm")).not.toBeInTheDocument();
  });

  it("offers the key actions with the rotate link alone", () => {
    renderPanel(["rotate-unlock-key"]);

    expect(screen.getByText("Show Unlock Key")).toBeInTheDocument();
    expect(screen.getByText("Rotate Unlock Key")).toBeInTheDocument();
    expect(screen.queryByText("Unlock Swarm")).not.toBeInTheDocument();
  });

  it("offers unlocking with the unlock link alone", () => {
    renderPanel(["unlock"]);

    expect(screen.getByText("Unlock Swarm")).toBeInTheDocument();
    expect(screen.queryByText("Rotate Unlock Key")).not.toBeInTheDocument();
  });
});
