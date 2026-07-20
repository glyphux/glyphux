import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Image as ImageIcon, Search, Upload } from "lucide-react";
import { GlyphuxApiError, type MediaItem } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useToast } from "@/lib/toast-context";
import { usePagination } from "@/lib/use-pagination";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { Skeleton } from "@/components/ui/skeleton";
import { Pagination } from "@/components/ui/pagination";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { IMAGE_MIME, formatBytes, matchesSearch } from "./MediaLibraryPage";

const PAGE_SIZE = 12;

/** A real browse/search/upload/select picker over the Phase-1 media
 * library — reused (not reinvented) by any `FieldMedia`-kind field
 * (`pages/content/fields.tsx`'s `FieldControl`, which both Layer-1
 * content-type fields and the Layer-2 builder's `PropsEditor.tsx` already
 * share). Talks to the library through the exact same `sdk-js`
 * `client.media` methods `MediaLibraryPage.tsx` uses for browsing/
 * uploading — no new backend endpoints, no raw `fetch`. Stock-media/
 * external providers are explicitly out of scope (ticket 3.7, deferred);
 * this is existing-library-only. */
export function MediaPicker({
  open,
  onOpenChange,
  onSelect,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSelect: (item: MediaItem) => void;
}) {
  const { toast } = useToast();
  const [items, setItems] = useState<MediaItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);
  const [uploading, setUploading] = useState(false);
  const [search, setSearch] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  const load = useCallback(() => {
    setLoading(true);
    setError(undefined);
    client.media
      .list()
      .then(setItems)
      .catch(setError)
      .finally(() => setLoading(false));
  }, []);

  // Fetch only while the dialog is actually open — a hidden picker (e.g. a
  // FieldControl not yet interacted with) shouldn't hit the API at all,
  // and reopening always shows the current library state rather than a
  // stale snapshot from the first mount.
  useEffect(() => {
    if (!open) return;
    setSearch("");
    load();
  }, [open, load]);

  const filteredItems = useMemo(() => items.filter((it) => matchesSearch(it, search)), [items, search]);
  const { page, setPage, totalPages, pageItems } = usePagination(filteredItems, PAGE_SIZE);

  const onFilesSelected = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const file of Array.from(files)) {
        await client.media.upload(file, file.name);
      }
      toast({ title: files.length > 1 ? `Uploaded ${files.length} files` : "Uploaded", variant: "success" });
      load();
    } catch (err) {
      toast({
        title: "Upload failed",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setUploading(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  };

  const choose = (item: MediaItem) => {
    onSelect(item);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Choose media</DialogTitle>
          <DialogDescription>Select an existing asset, or upload a new one.</DialogDescription>
        </DialogHeader>

        <div className="flex items-center justify-between gap-4">
          <div className="relative max-w-sm flex-1">
            <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2" />
            <Input
              type="search"
              placeholder="Search by filename, alt text, or tag…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="pl-9"
              aria-label="Search media"
            />
          </div>
          <input
            ref={fileInput}
            type="file"
            multiple
            accept="image/png,image/jpeg,image/gif"
            className="sr-only"
            tabIndex={-1}
            aria-hidden="true"
            onChange={(e) => onFilesSelected(e.target.files)}
          />
          <Button type="button" variant="outline" disabled={uploading} onClick={() => fileInput.current?.click()}>
            <Upload /> {uploading ? "Uploading…" : "Upload"}
          </Button>
        </div>

        {loading && (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
            {Array.from({ length: 8 }).map((_, i) => (
              <Skeleton key={i} className="aspect-square w-full" />
            ))}
          </div>
        )}
        {!loading && error !== undefined && <ErrorState error={error} onRetry={load} />}
        {!loading && !error && items.length === 0 && (
          <EmptyState
            icon={<ImageIcon />}
            title="No media yet"
            description="Upload PNG, JPEG, or GIF files to build your library."
          />
        )}
        {!loading && !error && items.length > 0 && filteredItems.length === 0 && (
          <EmptyState icon={<Search />} title="No matches" description={`Nothing matches "${search}".`} />
        )}
        {!loading && !error && filteredItems.length > 0 && (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4">
            {pageItems.map((it) => (
              <Card key={it.id} className="gap-2 p-2">
                <button
                  type="button"
                  className="bg-muted focus-visible:ring-ring flex aspect-square items-center justify-center overflow-hidden rounded-md outline-none focus-visible:ring-2"
                  onClick={() => choose(it)}
                  aria-label={`Select ${it.filename}`}
                >
                  {IMAGE_MIME.has(it.mime_type) ? (
                    <img
                      src={`/api/v0/media/${encodeURIComponent(it.id)}/file?w=160&h=160`}
                      alt={it.alt_text || it.filename}
                      className="size-full object-cover"
                    />
                  ) : (
                    <ImageIcon className="text-muted-foreground size-8" aria-hidden="true" />
                  )}
                </button>
                <p className="truncate text-small font-medium" title={it.filename}>
                  {it.filename}
                </p>
                <p className="text-muted-foreground text-small">{formatBytes(it.size_bytes)}</p>
              </Card>
            ))}
          </div>
        )}

        {!loading && !error && filteredItems.length > 0 && (
          <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
        )}
      </DialogContent>
    </Dialog>
  );
}
