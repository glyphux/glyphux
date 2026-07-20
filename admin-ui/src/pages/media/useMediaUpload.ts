import { useRef, useState } from "react";
import { GlyphuxApiError } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useToast } from "@/lib/toast-context";

/** The upload flow shared by every place that lets an operator add new
 * media asset(s) to the library: `MediaLibraryPage`'s own "Upload" button
 * and `MediaPicker`'s in-dialog upload — one `client.media.upload` call
 * per selected file, a single success/failure toast, and always clearing
 * the file input afterward so the same file can be reselected. Extracted
 * here (rather than left copy-pasted between the two call sites) because
 * this is the actual upload logic, not just a formatting/predicate helper
 * — the same drift risk `formatBytes`/`matchesSearch`/`IMAGE_MIME` were
 * already exported to avoid. */
export function useMediaUpload(onUploaded: () => void) {
  const [uploading, setUploading] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { toast } = useToast();

  const onFilesSelected = async (files: FileList | null) => {
    if (!files || files.length === 0) return;
    setUploading(true);
    try {
      for (const file of Array.from(files)) {
        await client.media.upload(file, file.name);
      }
      toast({ title: files.length > 1 ? `Uploaded ${files.length} files` : "Uploaded", variant: "success" });
      onUploaded();
    } catch (err) {
      toast({
        title: "Upload failed",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setUploading(false);
      if (fileInputRef.current) fileInputRef.current.value = "";
    }
  };

  return { uploading, fileInputRef, onFilesSelected };
}
