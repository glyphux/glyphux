import { useCallback, useEffect, useRef, useState } from "react";
import { Image as ImageIcon, Trash2, Upload } from "lucide-react";
import { GlyphuxApiError, type MediaItem } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { Skeleton } from "@/components/ui/skeleton";
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

export function MediaLibraryPage() {
  const { user } = useAuth();
  const { toast } = useToast();
  const canWrite = allows(user?.role, "media:write");

  const [items, setItems] = useState<MediaItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);
  const [uploading, setUploading] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<MediaItem | undefined>(undefined);
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
      {!loading && !error && items.length > 0 && (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
          {items.map((it) => (
            <Card key={it.id} className="gap-2 p-3">
              <div className="bg-muted flex aspect-square items-center justify-center overflow-hidden rounded-md">
                {IMAGE_MIME.has(it.mime_type) ? (
                  <img
                    src={`/api/v0/media/${encodeURIComponent(it.id)}/file?w=240&h=240`}
                    alt={it.alt_text || it.filename}
                    className="size-full object-cover"
                  />
                ) : (
                  <ImageIcon className="text-muted-foreground size-8" aria-hidden="true" />
                )}
              </div>
              <p className="truncate text-small font-medium" title={it.filename}>
                {it.filename}
              </p>
              <p className="text-muted-foreground text-small">{formatBytes(it.size_bytes)}</p>
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
    </div>
  );
}
