import { LoadingPage, LoadingDetail } from "./LoadingSkeleton";
import { render } from "@testing-library/react";
import { describe, it, expect } from "vitest";

describe("LoadingPage", () => {
  it("renders skeleton elements", () => {
    const { container } = render(<LoadingPage />);
    expect(container.querySelector(".animate-pulse")).toBeInTheDocument();
  });
});

describe("LoadingDetail", () => {
  it("renders skeleton elements", () => {
    const { container } = render(<LoadingDetail />);
    expect(container.querySelector(".animate-pulse")).toBeInTheDocument();
  });
});
