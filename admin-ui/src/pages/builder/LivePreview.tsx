import { useEditor } from "@craftjs/core";
import { useEffect, useState } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Spinner } from "@/components/ui/spinner";
import { ErrorState } from "@/components/layout/ErrorState";
import { client } from "@/lib/client";
import { nodeTreeToLayout } from "./serialize";

/** How long to wait after the last draft change before actually calling
 * preview() — every keystroke in a props field or every drag-drop otherwise
 * re-serializes the whole node tree and fires a new request; debouncing
 * keeps a fast typist from queuing a preview render per keystroke. */
export const PREVIEW_DEBOUNCE_MS = 400;

/** Ticket P4.5: "editor renders the same composition the API serves" — a
 * REAL, theme-accurate preview of the current in-editor draft, rendered
 * server-side through the actual themes/starter theme
 * (LayoutsResource.preview, POST /api/v0/layouts/preview) and embedded via
 * an iframe's srcDoc. Deliberately not a second, admin-ui-side
 * reimplementation of block-to-HTML rendering — that would drift from what
 * a real visitor sees, defeating the entire point. Complements (does not
 * replace) TreeView's plain structural read-out: this shows what the draft
 * *renders as*; TreeView shows what's *placed where*.
 *
 * Re-serializes and re-requests a preview on every editor state change,
 * debounced by PREVIEW_DEBOUNCE_MS so rapid edits (typing in a prop field,
 * dragging) don't fire one request per change. Never mutates anything
 * server-side — POST /api/v0/layouts/preview performs no persistence, so
 * this can run continuously against an unsaved draft with no risk of
 * clobbering what's actually live. */
export function LivePreview() {
  const { serialized } = useEditor((_state, query) => ({ serialized: query.getSerializedNodes() }));
  const [html, setHtml] = useState<string | undefined>(undefined);
  const [error, setError] = useState<unknown>(undefined);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const timer = setTimeout(() => {
      const layout = nodeTreeToLayout(serialized);
      client.layouts
        .preview(layout)
        .then((result) => {
          if (cancelled) return;
          setHtml(result.html);
          setError(undefined);
        })
        .catch((err: unknown) => {
          if (cancelled) return;
          setError(err);
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [serialized]);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle>Live preview</CardTitle>
        {loading && <Spinner label="Rendering preview" />}
      </CardHeader>
      <CardContent>
        {error ? (
          <ErrorState error={error} />
        ) : (
          // sandbox="" (no allow-scripts, no allow-same-origin): the
          // rendered HTML embeds untrusted block Props (starter's own
          // html/template auto-escaping already neutralizes markup
          // injection, but the iframe itself carries no script/DOM-access
          // privileges either, defense in depth).
          <iframe
            title="Live preview"
            srcDoc={html ?? ""}
            sandbox=""
            className="h-[600px] w-full rounded border bg-white"
          />
        )}
      </CardContent>
    </Card>
  );
}
