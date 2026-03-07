import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { LoadingPage, LoadingDetail } from "./LoadingSkeleton";

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
