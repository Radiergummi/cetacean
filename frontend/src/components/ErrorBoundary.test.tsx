import { describe, it, expect, vi, beforeAll, afterAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import ErrorBoundary from "./ErrorBoundary";

let shouldThrow = false;

function ThrowingComponent() {
  if (shouldThrow) throw new Error("Test error");
  return <div>Content</div>;
}

// Suppress console.error for expected errors
const originalError = console.error;
beforeAll(() => {
  console.error = vi.fn();
});
afterAll(() => {
  console.error = originalError;
});

describe("ErrorBoundary", () => {
  it("renders children when no error", () => {
    shouldThrow = false;
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Content")).toBeInTheDocument();
  });

  it("renders error UI when child throws", () => {
    shouldThrow = true;
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByText("Test error")).toBeInTheDocument();
  });

  it("recovers when try again is clicked", () => {
    shouldThrow = true;
    render(
      <ErrorBoundary>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();

    // Stop throwing before clicking "Try again"
    shouldThrow = false;
    fireEvent.click(screen.getByText("Try again"));

    expect(screen.getByText("Content")).toBeInTheDocument();
  });
});
