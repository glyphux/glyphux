import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Image as ImageIcon, Search, Trash2, Upload } from "lucide-react";
import { GlyphuxApiError, type MediaItem } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { usePagination } from "@/lib/use-pagination";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { Skeleton } from "@/components/ui/skeleton";
import { Pagination } from "@/components/ui/pagination";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

const IMAGE_MIME = new Set(["image/png", "image/jpeg", "image/gif"]);
const PAGE_SIZE = 20;

/** Splits a comma-separated tags input into a trimmed, non-empty list —
 * the inverse of joining `MediaItem.tags` with ", " for display. */
function parseTags(input: string): string[] {
  return input
    .split(",")
    .map((t) => t.trim())
    .filter((t) => t.length > 0);
}

/** Matches an item against a search query across filename, alt text, and
 * tags — client-side, since the library's list endpoint returns every item
 * unpaginated (internal/media.API.List) at a scale this is fine for. */
function matchesSearch(item: MediaItem, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  return (
    item.filename.toLowerCase().includes(q) ||
    item.alt_text.toLowerCase().includes(q) ||
    item.tags.some((t) => t.toLowerCase().includes(q))
  );
}

export function MediaLibraryPage() {
  const { user } = useAuth();
  const { toast } = useToast();
  const canWrite = allows(user?.role, "media:write");

  const [items, setItems] = useState<MediaItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);
  const [uploading, setUploading] = useState(false);
  const [search, setSearch] = useState("");
  const [pendingDelete, setPendingDelete] = useState<MediaItem | undefined>(undefined);
  const [detail, setDetail] = useState<MediaItem | undefined>(undefined);
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

  useEffect(load, [load]);

  const filteredItems = useMemo(() => items.filter((it) => matchesSearch(it, search)), [items, search]);

  // usePagination resets to page 1 whenever the array it's given is a new
  // reference — which `filteredItems` is on every search-text change or
  // reload, so a stale page number never lands on an out-of-range (now
  // empty) page without a separate effect here.
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

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    try {
      await client.media.delete(pendingDelete.id);
      toast({ title: "Deleted", variant: "success" });
      setPendingDelete(undefined);
      if (detail?.id === pendingDelete.id) setDetail(undefined);
      load();
    } catch (err) {
      toast({
        title: "Couldn't delete this asset",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display">Media</h1>
          <p className="text-muted-foreground text-body mt-1">Upload and manage your media library.</p>
        </div>
        {canWrite && (
          <>
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
            {/* A real <button> (not a styled <label>) triggering the hidden
             * input via ref — a <label for=...> has no implicit ARIA
             * button role, so it wouldn't be announced or keyboard-operable
             * as a control (WCAG 2.1 AA, PRD §5.7). */}
            <Button type="button" disabled={uploading} onClick={() => fileInput.current?.click()}>
              <Upload /> {uploading ? "Uploading…" : "Upload"}
            </Button>
          </>
        )}
      </div>

      {!loading && !error && items.length > 0 && (
        <div className="relative max-w-sm">
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
      )}

      {loading && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {Array.from({ length: 10 }).map((_, i) => (
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
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {pageItems.map((it) => (
            <Card key={it.id} className="gap-2 p-3">
              <button
                type="button"
                className="bg-muted focus-visible:ring-ring flex aspect-square items-center justify-center overflow-hidden rounded-md outline-none focus-visible:ring-2"
                onClick={() => setDetail(it)}
                aria-label={`View ${it.filename}`}
              >
                {IMAGE_MIME.has(it.mime_type) ? (
                  <img
                    src={`/api/v0/media/${encodeURIComponent(it.id)}/file?w=240&h=240`}
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
              {it.tags.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {it.tags.map((tag) => (
                    <Badge key={tag} variant="secondary">
                      {tag}
                    </Badge>
                  ))}
                </div>
              )}
              {canWrite && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-destructive justify-start px-0"
                  onClick={() => setPendingDelete(it)}
                >
                  <Trash2 /> Delete
                </Button>
              )}
            </Card>
          ))}
        </div>
      )}

      {!loading && !error && filteredItems.length > 0 && (
        <Pagination page={page} totalPages={totalPages} onPageChange={setPage} />
      )}

      <Dialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete "{pendingDelete?.filename}"?</DialogTitle>
            <DialogDescription>This permanently deletes the file. This cannot be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(undefined)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmDelete}>
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <MediaDetailDialog
        item={detail}
        canWrite={canWrite}
        onOpenChange={(open) => !open && setDetail(undefined)}
        onSaved={(updated) => {
          setDetail(updated);
          setItems((prev) => prev.map((it) => (it.id === updated.id ? updated : it)));
        }}
        onDelete={(it) => setPendingDelete(it)}
      />
    </div>
  );
}

/** The media detail view — a real preview (larger image than the grid
 * thumbnail) plus, for writers, inline editing of alt text/tags/source/
 * attribution. Deliberately just these four fields (PRD §11.4's metadata
 * set): this is a library management page, not the Phase-4 in-builder
 * picker or an image editor. */
function MediaDetailDialog({
  item,
  canWrite,
  onOpenChange,
  onSaved,
  onDelete,
}: {
  item: MediaItem | undefined;
  canWrite: boolean;
  onOpenChange: (open: boolean) => void;
  onSaved: (updated: MediaItem) => void;
  onDelete: (item: MediaItem) => void;
}) {
  const { toast } = useToast();
  const [altText, setAltText] = useState("");
  const [tagsInput, setTagsInput] = useState("");
  const [source, setSource] = useState("");
  const [attribution, setAttribution] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!item) return;
    setAltText(item.alt_text);
    setTagsInput(item.tags.join(", "));
    setSource(item.source);
    setAttribution(item.attribution);
  }, [item]);

  if (!item) return null;

  const onSave = async () => {
    setSaving(true);
    try {
      const updated = await client.media.updateMetadata(item.id, {
        alt_text: altText,
        tags: parseTags(tagsInput),
        source,
        attribution,
      });
      toast({ title: "Saved", variant: "success" });
      onSaved(updated);
    } catch (err) {
      toast({
        title: "Couldn't save changes",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={!!item} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{item.filename}</DialogTitle>
          <DialogDescription>
            {item.width}×{item.height} · {formatBytes(item.size_bytes)} · {item.mime_type}
          </DialogDescription>
        </DialogHeader>

        <div className="bg-muted flex max-h-96 items-center justify-center overflow-hidden rounded-md">
          {IMAGE_MIME.has(item.mime_type) ? (
            <img
              src={`/api/v0/media/${encodeURIComponent(item.id)}/file`}
              alt={item.alt_text || item.filename}
              className="max-h-96 max-w-full object-contain"
            />
          ) : (
            <ImageIcon className="text-muted-foreground size-16" aria-hidden="true" />
          )}
        </div>

        {canWrite ? (
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="media-alt-text">Alt text</Label>
              <Input id="media-alt-text" value={altText} onChange={(e) => setAltText(e.target.value)} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="media-tags">Tags</Label>
              <Input
                id="media-tags"
                placeholder="comma, separated, tags"
                value={tagsInput}
                onChange={(e) => setTagsInput(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="media-source">Source</Label>
              <Input id="media-source" value={source} onChange={(e) => setSource(e.target.value)} />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="media-attribution">Attribution</Label>
              <Input id="media-attribution" value={attribution} onChange={(e) => setAttribution(e.target.value)} />
            </div>
          </div>
        ) : (
          <div className="text-body flex flex-col gap-2">
            {item.alt_text && (
              <p>
                <span className="font-medium">Alt text:</span> {item.alt_text}
              </p>
            )}
            {item.tags.length > 0 && (
              <div className="flex flex-wrap items-center gap-1">
                <span className="font-medium">Tags:</span>
                {item.tags.map((tag) => (
                  <Badge key={tag} variant="secondary">
                    {tag}
                  </Badge>
                ))}
              </div>
            )}
            {item.attribution && (
              <p>
                <span className="font-medium">Attribution:</span> {item.attribution}
              </p>
            )}
          </div>
        )}

        <DialogFooter>
          {canWrite && (
            <Button
              variant="ghost"
              className="text-destructive mr-auto"
              onClick={() => onDelete(item)}
              type="button"
            >
              <Trash2 /> Delete
            </Button>
          )}
          {canWrite && (
            <Button onClick={onSave} disabled={saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
