import { useEffect, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { DndContext, PointerSensor, KeyboardSensor, closestCenter, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { SortableContext, arrayMove, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { GripVertical, Plus, Trash2 } from "lucide-react";
import { GlyphuxApiError, type ContentType, type FieldType } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useContentTypes } from "@/lib/use-content-types";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";

const FIELD_TYPES: FieldType[] = ["string", "richtext", "number", "boolean", "date", "relation", "media"];

interface FieldRow {
  id: string;
  name: string;
  type: FieldType;
  required: boolean;
  localized: boolean;
  to?: string;
}

function newRow(): FieldRow {
  return { id: crypto.randomUUID(), name: "", type: "string", required: false, localized: false };
}

function SortableRow({
  row,
  otherTypeNames,
  onChange,
  onRemove,
}: {
  row: FieldRow;
  otherTypeNames: string[];
  onChange: (row: FieldRow) => void;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: row.id });
  const style = { transform: CSS.Transform.toString(transform), transition, opacity: isDragging ? 0.6 : 1 };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className="border-border bg-card flex flex-col gap-3 rounded-lg border p-3 sm:flex-row sm:items-end"
    >
      <button
        type="button"
        className="text-muted-foreground hover:text-foreground flex items-center self-start pt-6 sm:self-auto sm:pt-0"
        aria-label={`Reorder field ${row.name || "(unnamed)"}`}
        {...attributes}
        {...listeners}
      >
        <GripVertical className="size-4" />
      </button>

      <div className="flex flex-1 flex-col gap-1.5">
        <Label htmlFor={`field-name-${row.id}`}>Field name</Label>
        <Input
          id={`field-name-${row.id}`}
          value={row.name}
          onChange={(e) => onChange({ ...row, name: e.target.value })}
          placeholder="title"
          required
        />
      </div>

      <div className="flex flex-1 flex-col gap-1.5">
        <Label htmlFor={`field-type-${row.id}`}>Type</Label>
        <Select value={row.type} onValueChange={(v) => onChange({ ...row, type: v as FieldType })}>
          <SelectTrigger id={`field-type-${row.id}`}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {FIELD_TYPES.map((t) => (
              <SelectItem key={t} value={t}>
                {t}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {row.type === "relation" && (
        <div className="flex flex-1 flex-col gap-1.5">
          <Label htmlFor={`field-to-${row.id}`}>Relates to</Label>
          <Select value={row.to ?? ""} onValueChange={(v) => onChange({ ...row, to: v })}>
            <SelectTrigger id={`field-to-${row.id}`}>
              <SelectValue placeholder="Choose a content type" />
            </SelectTrigger>
            <SelectContent>
              {otherTypeNames.map((t) => (
                <SelectItem key={t} value={t}>
                  {t}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}

      <div className="flex items-center gap-2 pb-1.5">
        <Checkbox
          id={`field-required-${row.id}`}
          checked={row.required}
          onCheckedChange={(v) => onChange({ ...row, required: v === true })}
        />
        <Label htmlFor={`field-required-${row.id}`} className="font-normal">
          Required
        </Label>
      </div>
      <div className="flex items-center gap-2 pb-1.5">
        <Checkbox
          id={`field-localized-${row.id}`}
          checked={row.localized}
          onCheckedChange={(v) => onChange({ ...row, localized: v === true })}
        />
        <Label htmlFor={`field-localized-${row.id}`} className="font-normal">
          Localized
        </Label>
      </div>

      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={`Remove field ${row.name || "(unnamed)"}`}
        onClick={onRemove}
      >
        <Trash2 className="text-destructive" />
      </Button>
    </div>
  );
}

export function ContentTypeFormPage() {
  const { name } = useParams<{ name?: string }>();
  const isEdit = !!name;
  const navigate = useNavigate();
  const { toast } = useToast();
  const { types, loading: typesLoading } = useContentTypes();

  const [typeName, setTypeName] = useState(name ?? "");
  const [rows, setRows] = useState<FieldRow[]>(isEdit ? [] : [newRow()]);
  const [hydrated, setHydrated] = useState(!isEdit);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!isEdit || hydrated || typesLoading) return;
    const existing: ContentType | undefined = name ? types[name] : undefined;
    if (existing) {
      setRows(
        Object.entries(existing.fields).map(([fieldName, field]) => ({
          id: crypto.randomUUID(),
          name: fieldName,
          type: field.type,
          required: !!field.required,
          localized: !!field.localized,
          to: field.to,
        })),
      );
      setHydrated(true);
    } else if (Object.keys(types).length > 0) {
      // Content types have loaded and this name genuinely isn't among them.
      setError(`Content type "${name}" was not found.`);
      setHydrated(true);
    }
  }, [isEdit, hydrated, typesLoading, types, name]);

  const sensors = useSensors(useSensor(PointerSensor), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));

  const onDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    setRows((prev) => {
      const oldIndex = prev.findIndex((r) => r.id === active.id);
      const newIndex = prev.findIndex((r) => r.id === over.id);
      return arrayMove(prev, oldIndex, newIndex);
    });
  };

  const otherTypeNames = Object.keys(types).filter((t) => t !== name);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(undefined);

    const trimmedName = typeName.trim();
    if (!trimmedName) {
      setError("Content type name is required.");
      return;
    }
    const seen = new Set<string>();
    for (const row of rows) {
      const fieldName = row.name.trim();
      if (!fieldName) {
        setError("Every field needs a name.");
        return;
      }
      if (seen.has(fieldName)) {
        setError(`Field name "${fieldName}" is used more than once.`);
        return;
      }
      seen.add(fieldName);
      if (row.type === "relation" && !row.to) {
        setError(`Field "${fieldName}" is a relation and needs a target content type.`);
        return;
      }
    }

    const fields: ContentType["fields"] = {};
    for (const row of rows) {
      fields[row.name.trim()] = {
        type: row.type,
        required: row.required || undefined,
        localized: row.localized || undefined,
        to: row.type === "relation" ? row.to : undefined,
      };
    }

    setSubmitting(true);
    try {
      await client.contentTypes.define(trimmedName, { fields });
      toast({ title: `Saved "${trimmedName}"`, variant: "success" });
      navigate("/content-types");
    } catch (err) {
      setError(err instanceof GlyphuxApiError ? err.message : "Could not save this content type.");
    } finally {
      setSubmitting(false);
    }
  };

  if (isEdit && !hydrated) {
    return (
      <div className="flex justify-center py-16">
        <Spinner label="Loading content type" />
      </div>
    );
  }

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-display">{isEdit ? `Edit "${name}"` : "New content type"}</h1>
        <p className="text-muted-foreground text-body mt-1">
          Declare the fields items of this type will have. Changing field types on an existing content type can
          invalidate existing items.
        </p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-6" noValidate>
        {error && (
          <Alert variant="destructive">
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        )}

        <Card>
          <CardHeader>
            <CardTitle>Basics</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex max-w-sm flex-col gap-1.5">
              <Label htmlFor="type-name">Content type name</Label>
              <Input
                id="type-name"
                value={typeName}
                onChange={(e) => setTypeName(e.target.value)}
                disabled={isEdit}
                placeholder="post"
                required
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Fields</CardTitle>
          </CardHeader>
          <CardContent>
            {rows.length === 0 && <p className="text-muted-foreground text-body">No fields yet. Add one below.</p>}
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
              <SortableContext items={rows.map((r) => r.id)} strategy={verticalListSortingStrategy}>
                <div className="flex flex-col gap-3">
                  {rows.map((row) => (
                    <SortableRow
                      key={row.id}
                      row={row}
                      otherTypeNames={otherTypeNames}
                      onChange={(next) => setRows((prev) => prev.map((r) => (r.id === next.id ? next : r)))}
                      onRemove={() => setRows((prev) => prev.filter((r) => r.id !== row.id))}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
            <Button type="button" variant="outline" className="mt-3 self-start" onClick={() => setRows((prev) => [...prev, newRow()])}>
              <Plus /> Add field
            </Button>
          </CardContent>
        </Card>

        <div className="flex gap-2">
          <Button type="submit" disabled={submitting}>
            {submitting ? "Saving…" : "Save content type"}
          </Button>
          <Button type="button" variant="outline" onClick={() => navigate("/content-types")}>
            Cancel
          </Button>
        </div>
      </form>
    </div>
  );
}
