import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Navigate, useNavigate, useParams } from "react-router-dom";
import { GlyphuxApiError, type ContentItem, type ContentVersion } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useContentTypes } from "@/lib/use-content-types";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { FieldControl } from "./fields";

export function ContentFormPage() {
  const { type, id } = useParams<{ type: string; id?: string }>();
  const isEdit = !!id;
  const navigate = useNavigate();
  const { toast } = useToast();
  const { user } = useAuth();
  const { types, loading: typesLoading } = useContentTypes();

  const [data, setData] = useState<Record<string, unknown>>({});
  const [item, setItem] = useState<ContentItem | undefined>(undefined);
  const [loading, setLoading] = useState(isEdit);
  const [loadError, setLoadError] = useState<unknown>(undefined);
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState<string | undefined>(undefined);
  const [versions, setVersions] = useState<ContentVersion[]>([]);
  const [versionsLoaded, setVersionsLoaded] = useState(false);

  const loadItem = useCallback(() => {
    if (!type || !id) return;
    setLoading(true);
    setLoadError(undefined);
    client.content
      .get(type, id)
      .then((it) => {
        setItem(it);
        setData(it.data);
      })
      .catch(setLoadError)
      .finally(() => setLoading(false));
  }, [type, id]);

  useEffect(loadItem, [loadItem]);

  const loadVersions = useCallback(() => {
    if (!type || !id) return;
    client.content
      .listVersions(type, id)
      .then(setVersions)
      .catch(() => setVersions([]))
      .finally(() => setVersionsLoaded(true));
  }, [type, id]);

  if (!type) return <Navigate to="/content-types" replace />;

  const contentType = types[type];
  const canWrite = allows(user?.role, "content:write");
  const canPublish = allows(user?.role, "content:publish");

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setFormError(undefined);
    setSubmitting(true);
    try {
      if (isEdit && id) {
        const updated = await client.content.update(type, id, data);
        setItem(updated);
        setData(updated.data);
        toast({ title: "Saved", variant: "success" });
      } else {
        const created = await client.content.create(type, data);
        toast({ title: "Created", variant: "success" });
        navigate(`/content/${encodeURIComponent(type)}/${encodeURIComponent(created.id)}`, { replace: true });
      }
    } catch (err) {
      setFormError(err instanceof GlyphuxApiError ? err.message : "Could not save this item.");
    } finally {
      setSubmitting(false);
    }
  };

  const togglePublish = async () => {
    if (!item || !id) return;
    setSubmitting(true);
    try {
      const updated = item.status === "published" ? await client.content.unpublish(type, id) : await client.content.publish(type, id);
      setItem(updated);
      toast({ title: updated.status === "published" ? "Published" : "Unpublished", variant: "success" });
    } catch (err) {
      toast({
        title: "Couldn't update publish status",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setSubmitting(false);
    }
  };

  const rollback = async (version: number) => {
    if (!id) return;
    try {
      const updated = await client.content.rollback(type, id, version);
      setItem(updated);
      setData(updated.data);
      toast({ title: `Restored version ${version}`, variant: "success" });
      loadVersions();
    } catch (err) {
      toast({
        title: "Couldn't restore this version",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    }
  };

  if (typesLoading || loading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner label="Loading" />
      </div>
    );
  }

  if (!contentType) {
    return (
      <Alert variant="destructive">
        <AlertDescription>"{type}" is not a declared content type.</AlertDescription>
      </Alert>
    );
  }

  if (isEdit && loadError) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          {loadError instanceof GlyphuxApiError ? loadError.message : "Could not load this item."}
        </AlertDescription>
      </Alert>
    );
  }

  const form = (
    <form onSubmit={onSubmit} className="flex flex-col gap-6" noValidate>
      {formError && (
        <Alert variant="destructive">
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      )}
      <Card>
        <CardHeader>
          <CardTitle>Fields</CardTitle>
        </CardHeader>
        <CardContent>
          {Object.entries(contentType.fields).map(([fieldName, field]) => {
            const fieldId = `field-${fieldName}`;
            if (field.localized) {
              const localeMap = (data[fieldName] as Record<string, unknown>) ?? {};
              return (
                <div key={fieldName} className="flex flex-col gap-1.5">
                  <Label htmlFor={fieldId}>
                    {fieldName}
                    {field.required && <span className="text-destructive"> *</span>}
                    <span className="text-muted-foreground font-normal"> (en)</span>
                  </Label>
                  <FieldControl
                    id={fieldId}
                    field={field}
                    value={localeMap.en}
                    onChange={(v) => setData((prev) => ({ ...prev, [fieldName]: { ...localeMap, en: v } }))}
                  />
                </div>
              );
            }
            return (
              <div key={fieldName} className="flex flex-col gap-1.5">
                <Label htmlFor={fieldId}>
                  {fieldName}
                  {field.required && <span className="text-destructive"> *</span>}
                </Label>
                <FieldControl
                  id={fieldId}
                  field={field}
                  value={data[fieldName]}
                  onChange={(v) => setData((prev) => ({ ...prev, [fieldName]: v }))}
                />
              </div>
            );
          })}
        </CardContent>
      </Card>

      {canWrite && (
        <div className="flex gap-2">
          <Button type="submit" disabled={submitting}>
            {submitting ? "Saving…" : isEdit ? "Save changes" : "Create"}
          </Button>
          <Button type="button" variant="outline" onClick={() => navigate(`/content/${encodeURIComponent(type)}`)}>
            Cancel
          </Button>
        </div>
      )}
    </form>
  );

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display capitalize">
            {isEdit ? `Edit ${type}` : `New ${type}`}
          </h1>
          {item && (
            <div className="mt-2 flex items-center gap-2">
              <Badge variant={item.status === "published" ? "success" : "secondary"}>{item.status}</Badge>
              <span className="text-muted-foreground text-small">version {item.version}</span>
            </div>
          )}
        </div>
        {isEdit && canPublish && item && (
          <Button variant="outline" onClick={togglePublish} disabled={submitting}>
            {item.status === "published" ? "Unpublish" : "Publish"}
          </Button>
        )}
      </div>

      {isEdit ? (
        <Tabs
          defaultValue="edit"
          onValueChange={(v) => {
            if (v === "versions" && !versionsLoaded) loadVersions();
          }}
        >
          <TabsList>
            <TabsTrigger value="edit">Edit</TabsTrigger>
            <TabsTrigger value="versions">Version history</TabsTrigger>
          </TabsList>
          <TabsContent value="edit">{form}</TabsContent>
          <TabsContent value="versions">
            {!versionsLoaded && (
              <div className="flex justify-center py-8">
                <Spinner label="Loading versions" />
              </div>
            )}
            {versionsLoaded && versions.length === 0 && (
              <p className="text-muted-foreground text-body">No version history yet.</p>
            )}
            {versionsLoaded && versions.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Version</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Created</TableHead>
                    <TableHead className="w-0" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {[...versions].reverse().map((v) => (
                    <TableRow key={v.version}>
                      <TableCell>{v.version}</TableCell>
                      <TableCell>
                        <Badge variant={v.status === "published" ? "success" : "secondary"}>{v.status}</Badge>
                      </TableCell>
                      <TableCell className="text-muted-foreground text-small">
                        {new Date(v.created_at).toLocaleString()}
                      </TableCell>
                      <TableCell>
                        {canWrite && item && v.version !== item.version && (
                          <Button variant="outline" size="sm" onClick={() => rollback(v.version)}>
                            Restore
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </TabsContent>
        </Tabs>
      ) : (
        form
      )}
    </div>
  );
}
