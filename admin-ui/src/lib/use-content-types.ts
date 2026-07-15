import { useCallback, useEffect, useState } from "react";
import type { ContentType } from "@glyphux/sdk";
import { client } from "./client";

interface State {
  types: Record<string, ContentType>;
  loading: boolean;
  error: unknown;
  reload: () => void;
}

/** Shared fetch of the declared content types — used by the sidebar nav (to
 * list content sections) and by the content list/form pages (to validate a
 * :type param and know its fields). Kept as its own hook rather than
 * duplicated fetches so every consumer agrees on the same data. */
export function useContentTypes(): State {
  const [types, setTypes] = useState<Record<string, ContentType>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    client.contentTypes
      .list()
      .then((result) => {
        if (!cancelled) setTypes(result);
      })
      .catch((err) => {
        if (!cancelled) setError(err);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [reloadKey]);

  const reload = useCallback(() => setReloadKey((k) => k + 1), []);

  return { types, loading, error, reload };
}
