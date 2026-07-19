import { describe, expect, it } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { usePagination } from "./use-pagination";

// Seam: the page-of-N slicing arithmetic that ContentListPage.tsx and
// MediaLibraryPage.tsx both used to reimplement independently
// (PAGE_SIZE const, page state, totalPages, .slice(...)) — extracted here
// so it's defined and tested exactly once.
describe("usePagination", () => {
  it("returns all items on page 1 when everything fits on one page", () => {
    const items = [1, 2, 3];
    const { result } = renderHook(() => usePagination(items, 10));
    expect(result.current.page).toBe(1);
    expect(result.current.totalPages).toBe(1);
    expect(result.current.pageItems).toEqual([1, 2, 3]);
  });

  it("slices items into pages of the given size", () => {
    const items = Array.from({ length: 25 }, (_, i) => i + 1);
    const { result } = renderHook(() => usePagination(items, 10));

    expect(result.current.totalPages).toBe(3);
    expect(result.current.pageItems).toEqual(Array.from({ length: 10 }, (_, i) => i + 1));

    act(() => result.current.setPage(2));
    expect(result.current.pageItems).toEqual(Array.from({ length: 10 }, (_, i) => i + 11));

    act(() => result.current.setPage(3));
    expect(result.current.pageItems).toEqual([21, 22, 23, 24, 25]);
  });

  it("treats an empty list as a single (empty) page, not zero pages", () => {
    const { result } = renderHook(() => usePagination([] as number[], 10));
    expect(result.current.totalPages).toBe(1);
    expect(result.current.pageItems).toEqual([]);
  });

  it("resets to page 1 when the items array is replaced (e.g. a new search/reload)", () => {
    const first = Array.from({ length: 25 }, (_, i) => i + 1);
    const { result, rerender } = renderHook(({ items }) => usePagination(items, 10), {
      initialProps: { items: first },
    });

    act(() => result.current.setPage(3));
    expect(result.current.page).toBe(3);

    const second = Array.from({ length: 5 }, (_, i) => i + 100);
    rerender({ items: second });

    expect(result.current.page).toBe(1);
    expect(result.current.pageItems).toEqual(second);
  });
});
