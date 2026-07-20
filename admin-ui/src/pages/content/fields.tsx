import { useEffect, useState } from "react";
import { Image as ImageIcon } from "lucide-react";
import type { ContentTypeField, MediaItem } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { IMAGE_MIME } from "@/pages/media/MediaLibraryPage";
import { MediaPicker } from "@/pages/media/MediaPicker";

/** One dynamic form control rendered from a content type's field
 * declaration. Handles the non-localized case directly; localized fields
 * are wrapped by `LocalizedField` below (this pass edits a single "en"
 * locale — see the slice's tracking note for why). */
export function FieldControl({
  id,
  field,
  value,
  onChange,
}: {
  id: string;
  field: ContentTypeField;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  switch (field.type) {
    case "string":
      return <Input id={id} value={typeof value === "string" ? value : ""} onChange={(e) => onChange(e.target.value)} />;
    case "richtext":
      return (
        <Textarea
          id={id}
          value={typeof value === "string" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
          rows={6}
        />
      );
    case "number":
      return (
        <Input
          id={id}
          type="number"
          value={typeof value === "number" ? String(value) : ""}
          onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
        />
      );
    case "boolean":
      return <Switch id={id} checked={value === true} onCheckedChange={onChange} />;
    case "date":
      return (
        <Input
          id={id}
          type="date"
          value={typeof value === "string" ? value : ""}
          onChange={(e) => onChange(e.target.value)}
        />
      );
    case "relation":
      return <RelationField id={id} to={field.to} value={value} onChange={onChange} />;
    case "media":
      return <MediaField id={id} value={value} onChange={onChange} />;
    default:
      return null;
  }
}

function RelationField({
  id,
  to,
  value,
  onChange,
}: {
  id: string;
  to: string | undefined;
  value: unknown;
  onChange: (v: unknown) => void;
}) {
  const [options, setOptions] = useState<{ id: string; label: string }[]>([]);

  useEffect(() => {
    if (!to) return;
    client.content
      .list(to)
      .then((items) =>
        setOptions(
          items.map((item) => ({
            id: item.id,
            label:
              typeof item.data.title === "string"
                ? item.data.title
                : item.id,
          })),
        ),
      )
      .catch(() => setOptions([]));
  }, [to]);

  return (
    <Select value={typeof value === "string" ? value : ""} onValueChange={onChange}>
      <SelectTrigger id={id}>
        <SelectValue placeholder={to ? `Choose a ${to}` : "No target type"} />
      </SelectTrigger>
      <SelectContent>
        {options.map((o) => (
          <SelectItem key={o.id} value={o.id}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

/** A `FieldMedia`-kind field's control — a thumbnail preview of whatever
 * media item `value` (its id) currently points to, a button that opens the
 * real media-library picker (`MediaPicker`, Ticket P4.9) to browse/search/
 * upload and choose a replacement, and a way to clear the selection. Used
 * both for Layer-1 content-type fields here and, via the shared
 * `FieldControl` above, for the Layer-2 builder's block props
 * (`pages/builder/PropsEditor.tsx`) — one control, not two, since both
 * consumers already share this same switch. */
function MediaField({ id, value, onChange }: { id: string; value: unknown; onChange: (v: unknown) => void }) {
  const selectedId = typeof value === "string" && value !== "" ? value : undefined;
  const [selected, setSelected] = useState<MediaItem | undefined>(undefined);
  const [pickerOpen, setPickerOpen] = useState(false);

  useEffect(() => {
    if (!selectedId) {
      setSelected(undefined);
      return;
    }
    let cancelled = false;
    client.media
      .get(selectedId)
      .then((item) => {
        if (!cancelled) setSelected(item);
      })
      .catch(() => {
        if (!cancelled) setSelected(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedId]);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-3">
        <div className="bg-muted flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-md">
          {selected && IMAGE_MIME.has(selected.mime_type) ? (
            <img
              src={`/api/v0/media/${encodeURIComponent(selected.id)}/file?w=64&h=64`}
              alt={selected.alt_text || selected.filename}
              className="size-full object-cover"
            />
          ) : (
            <ImageIcon className="text-muted-foreground size-6" aria-hidden="true" />
          )}
        </div>
        <div className="flex min-w-0 flex-col gap-1">
          <p className="truncate text-small font-medium" title={selected?.filename}>
            {selectedId ? (selected?.filename ?? "Loading…") : "No media selected"}
          </p>
          <div className="flex gap-2">
            <Button id={id} type="button" variant="outline" size="sm" onClick={() => setPickerOpen(true)}>
              {selectedId ? "Change" : "Choose media"}
            </Button>
            {selectedId && (
              <Button type="button" variant="ghost" size="sm" onClick={() => onChange(undefined)}>
                Clear
              </Button>
            )}
          </div>
        </div>
      </div>
      <MediaPicker
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onSelect={(item) => {
          setSelected(item);
          onChange(item.id);
        }}
      />
    </div>
  );
}
