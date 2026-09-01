import { LogTail } from "./LogTail";
import { toLogLine } from "@/components/log/log-utils";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

const lines = [
  { timestamp: "2026-08-30T10:00:00Z", message: "INFO started", stream: "stdout" as const },
  { timestamp: "2026-08-30T10:00:01Z", message: "ERROR boom", stream: "stderr" as const },
].map(toLogLine);

describe("LogTail", () => {
  it("renders the lines it is given", () => {
    render(
      <LogTail
        service="svc-a"
        lines={lines}
      />,
    );

    expect(screen.getByText(/started/)).toBeInTheDocument();
    expect(screen.getByText(/boom/)).toBeInTheDocument();
  });

  it("filters by level", () => {
    render(
      <LogTail
        service="svc-a"
        lines={lines}
      />,
    );

    fireEvent.change(screen.getByTitle(/level/i), { target: { value: "error" } });

    expect(screen.queryByText(/started/)).not.toBeInTheDocument();
    expect(screen.getByText(/boom/)).toBeInTheDocument();
  });

  it("filters by a search term", () => {
    render(
      <LogTail
        service="svc-a"
        lines={lines}
      />,
    );

    fireEvent.change(screen.getByRole("searchbox"), { target: { value: "boom" } });

    expect(screen.queryByText(/started/)).not.toBeInTheDocument();
    expect(screen.getByText(/boom/)).toBeInTheDocument();
  });

  it("names the service it is tailing while nothing has arrived yet", () => {
    render(
      <LogTail
        service="svc-a"
        lines={[]}
      />,
    );

    expect(screen.getByText(/svc-a/)).toBeInTheDocument();
  });

  it("shows a failed read instead of an idle empty state", () => {
    render(
      <LogTail
        service="svc-a"
        lines={[]}
        error={new Error("service not found")}
      />,
    );

    expect(screen.getByText(/service not found/)).toBeInTheDocument();
  });
});
