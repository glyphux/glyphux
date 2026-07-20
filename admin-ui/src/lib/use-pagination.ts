import { useEffect, useState } from "react";

interface Pagination<T> {
  page: number;
  setPage: (page: number) => void;
  totalPages: number;
  pageItems: T[];
}

/** Shared page-of-N slicing over an already-loaded array — the exact
 * arithmetic ContentListPage.tsx and MediaLibraryPage.tsx each used to
 * reimplement independently (a PAGE_SIZE const, `useState(1)`,
 * `Math.max(1, Math.ceil(...))`, `.slice(...)`). Owns only the "which page
 * am I on, and what does that page's slice look like" concern; the
 * `ui/pagination.tsx` component stays purely presentational and the
 * consumer still owns fetching/filtering `items` itself.
 *
 * Resets to page 1 whenever `items` is replaced by a new array reference
 * (a fresh fetch completing, or a filter/search recomputing its result) —
 * so a stale page number never strands the user on a now-out-of-range,
 * empty page. Passing the same array reference back (no new data) does
 * not reset the page. */
export function usePagination<T>(items: T[], pageSize: number): Pagination<T> {
  const [page, setPage] = useState(1);

  useEffect(() => {
    setPage(1);
    // Only `items`, deliberately: a new array reference (reload/refilter)
    // should reset to page 1; `pageSize` never changes at runtime in
    // either current consumer, and including it would be one more thing
    // to reason about for no real behavior gain.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [items]);

  const totalPages = Math.max(1, Math.ceil(items.length / pageSize));
  const pageItems = items.slice((page - 1) * pageSize, page * pageSize);

  return { page, setPage, totalPages, pageItems };
}
