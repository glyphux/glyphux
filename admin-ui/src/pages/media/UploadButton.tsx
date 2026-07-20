import { Upload } from "lucide-react";
import { Button, type ButtonProps } from "@/components/ui/button";
import { useMediaUpload } from "./useMediaUpload";

/** The hidden-file-input-plus-button pairing every media-upload entry
 * point uses — `MediaLibraryPage`'s own "Upload" button and `MediaPicker`'s
 * in-dialog upload — sharing both the upload flow (`useMediaUpload`) and
 * this markup so the two never drift out of sync (e.g. one adding an
 * accepted MIME type the other forgets). `variant`/`size` pass straight
 * through to the underlying `Button` since the two call sites style it
 * differently (primary vs. outline). */
export function UploadButton({
  onUploaded,
  variant,
  size,
}: {
  onUploaded: () => void;
  variant?: ButtonProps["variant"];
  size?: ButtonProps["size"];
}) {
  const { uploading, fileInputRef, onFilesSelected } = useMediaUpload(onUploaded);

  return (
    <>
      <input
        ref={fileInputRef}
        type="file"
        multiple
        accept="image/png,image/jpeg,image/gif"
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        onChange={(e) => onFilesSelected(e.target.files)}
      />
      {/* A real <button> (not a styled <label>) triggering the hidden
       * input via ref — a <label for=...> has no implicit ARIA button
       * role, so it wouldn't be announced or keyboard-operable as a
       * control (WCAG 2.1 AA, PRD §5.7). */}
      <Button type="button" variant={variant} size={size} disabled={uploading} onClick={() => fileInputRef.current?.click()}>
        <Upload /> {uploading ? "Uploading…" : "Upload"}
      </Button>
    </>
  );
}
