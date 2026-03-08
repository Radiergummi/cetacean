import { describe, it, expect } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { useSort } from "./useSort";

interface Item {
  name: string;
  age: number;
}

const items: Item[] = [
  { name: "Charlie", age: 30 },
  { name: "Alice", age: 25 },
  { name: "Bob", age: 35 },
];

const accessors = {
  name: (i: Item) => i.name,
  age: (i: Item) => i.age,
};

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

describe("useSort", () => {
  it("returns items unsorted when no default key", () => {
    const { result } = renderHook(() => useSort(items, accessors), { wrapper });
    expect(result.current.sorted).toEqual(items);
    expect(result.current.sortKey).toBeUndefined();
  });

  it("sorts by default key ascending", () => {
    const { result } = renderHook(() => useSort(items, accessors, "name"), { wrapper });
    expect(result.current.sorted.map((i) => i.name)).toEqual(["Alice", "Bob", "Charlie"]);
    expect(result.current.sortDir).toBe("asc");
  });

  it("sorts numerically", () => {
    const { result } = renderHook(() => useSort(items, accessors, "age"), { wrapper });
    expect(result.current.sorted.map((i) => i.age)).toEqual([25, 30, 35]);
  });

  it("toggles direction on same key", () => {
    const { result } = renderHook(() => useSort(items, accessors, "name"), { wrapper });
    act(() => result.current.toggle("name"));
    expect(result.current.sortDir).toBe("desc");
    expect(result.current.sorted.map((i) => i.name)).toEqual(["Charlie", "Bob", "Alice"]);
  });

  it("switches key and resets to asc", () => {
    const { result } = renderHook(() => useSort(items, accessors, "name"), { wrapper });
    act(() => result.current.toggle("name")); // desc
    act(() => result.current.toggle("age")); // switch to age, asc
    expect(result.current.sortKey).toBe("age");
    expect(result.current.sortDir).toBe("asc");
    expect(result.current.sorted.map((i) => i.age)).toEqual([25, 30, 35]);
  });

  it("handles undefined values", () => {
    const data = [
      { name: "A", age: 1 },
      { name: undefined as unknown as string, age: 2 },
    ];
    const { result } = renderHook(() => useSort(data, accessors, "name"), { wrapper });
    expect(result.current.sorted).toHaveLength(2);
  });
});
