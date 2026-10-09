import { CAConfigPanel } from "./CAConfigPanel";
import type { SwarmInfo } from "@/api/types";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

const spec = {
  CAConfig: { NodeCertExpiry: 7776000000000000 },
} as unknown as SwarmInfo["swarm"]["Spec"];

describe("CAConfigPanel", () => {
  it("hides Force Rotate from a caller who cannot use it", () => {
    render(
      <CAConfigPanel
        spec={spec}
        rootRotationInProgress={false}
        canEdit={false}
        onSaved={() => {}}
      />,
    );

    expect(screen.queryByRole("button", { name: /Force Rotate/ })).not.toBeInTheDocument();
  });
});
