import { useEffect, useState } from "react";
import type { ContentTypeField } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Switch } from "@/components/ui/switch";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";

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

function MediaField({ id, value, onChange }: { id: string; value: unknown; onChange: (v: unknown) => void }) {
  const [options, setOptions] = useState<{ id: string; label: string }[]>([]);

  useEffect(() => {
    client.media
      .list()
      .then((items) => setOptions(items.map((item) => ({ id: item.id, label: item.filename }))))
      .catch(() => setOptions([]));
  }, []);

  return (
    <Select value={typeof value === "string" ? value : ""} onValueChange={onChange}>
      <SelectTrigger id={id}>
        <SelectValue placeholder="Choose a media asset" />
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
