import ErrorBoundary from "./ErrorBoundary";
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeAll, afterAll } from "vitest";

let shouldThrow = false;

function ThrowingComponent() {
  if (shouldThrow) {
    throw new Error("Test error");
  }

  return <div>Content</div>;
}

// Suppress console.error for expected errors
const originalError = console.error; // eslint-disable-line no-console
beforeAll(() => {
  console.error = vi.fn(); // eslint-disable-line no-console
});
afterAll(() => {
  console.error = originalError; // eslint-disable-line no-console
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

  it("renders compact inline fallback when inline prop is set", () => {
    shouldThrow = true;
    render(
      <ErrorBoundary inline>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.queryByText("Something went wrong")).not.toBeInTheDocument();
    expect(screen.getByText("Test error")).toBeInTheDocument();
    expect(screen.getByText("Retry")).toBeInTheDocument();
  });

  it("recovers from inline error on retry", () => {
    shouldThrow = true;
    render(
      <ErrorBoundary inline>
        <ThrowingComponent />
      </ErrorBoundary>,
    );
    expect(screen.getByText("Test error")).toBeInTheDocument();

    shouldThrow = false;
    fireEvent.click(screen.getByText("Retry"));

    expect(screen.getByText("Content")).toBeInTheDocument();
  });
});
