import { useCallback, useEffect, useState } from "react";
import { Link, Navigate, useParams } from "react-router-dom";
import { FileText, Plus, Trash2 } from "lucide-react";
import { GlyphuxApiError, type ContentItem } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useContentTypes } from "@/lib/use-content-types";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { ListSkeleton } from "@/components/layout/ListSkeleton";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

export function ContentListPage() {
  const { type } = useParams<{ type: string }>();
  const { user } = useAuth();
  const { toast } = useToast();
  const { types, loading: typesLoading } = useContentTypes();

  const [items, setItems] = useState<ContentItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);
  const [pendingDelete, setPendingDelete] = useState<ContentItem | undefined>(undefined);
  const [busyId, setBusyId] = useState<string | undefined>(undefined);

  const load = useCallback(() => {
    if (!type) return;
    setLoading(true);
    setError(undefined);
    client.content
      .list(type)
      .then(setItems)
      .catch(setError)
      .finally(() => setLoading(false));
  }, [type]);

  useEffect(load, [load]);

  if (!type) return <Navigate to="/content-types" replace />;
  if (!typesLoading && !types[type]) {
    return (
      <ErrorState
        error={new Error(`"${type}" is not a declared content type.`)}
      />
    );
  }

  const canWrite = allows(user?.role, "content:write");
  const canPublish = allows(user?.role, "content:publish");

  const titleField = type && types[type] ? Object.keys(types[type].fields).find((f) => f === "title") ?? Object.keys(types[type].fields)[0] : undefined;

  const togglePublish = async (item: ContentItem) => {
    setBusyId(item.id);
    try {
      if (item.status === "published") {
        await client.content.unpublish(type, item.id);
        toast({ title: "Unpublished", variant: "default" });
      } else {
        await client.content.publish(type, item.id);
        toast({ title: "Published", variant: "success" });
      }
      load();
    } catch (err) {
      toast({
        title: "Couldn't update publish status",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setBusyId(undefined);
    }
  };

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    setBusyId(pendingDelete.id);
    try {
      await client.content.delete(type, pendingDelete.id);
      toast({ title: "Deleted", variant: "success" });
      setPendingDelete(undefined);
      load();
    } catch (err) {
      toast({
        title: "Couldn't delete this item",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setBusyId(undefined);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display capitalize">{type}</h1>
          <p className="text-muted-foreground text-body mt-1">Content items of type "{type}".</p>
        </div>
        {canWrite && (
          <Button asChild>
            <Link to={`/content/${encodeURIComponent(type)}/new`}>
              <Plus /> New {type}
            </Link>
          </Button>
        )}
      </div>

      {(loading || typesLoading) && <ListSkeleton />}
      {!loading && error !== undefined && <ErrorState error={error} onRetry={load} />}
      {!loading && !error && items.length === 0 && (
        <EmptyState
          icon={<FileText />}
          title={`No ${type} items yet`}
          description="Create your first item of this type."
          action={
            canWrite && (
              <Button asChild>
                <Link to={`/content/${encodeURIComponent(type)}/new`}>
                  <Plus /> New {type}
                </Link>
              </Button>
            )
          }
        />
      )}
      {!loading && !error && items.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              {titleField && <TableHead>{titleField}</TableHead>}
              <TableHead>Status</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Updated</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((item) => (
              <TableRow key={item.id}>
                {titleField && (
                  <TableCell className="font-medium">
                    <Link to={`/content/${encodeURIComponent(type)}/${encodeURIComponent(item.id)}`} className="hover:underline">
                      {typeof item.data[titleField] === "string" ? (item.data[titleField] as string) : item.id}
                    </Link>
                  </TableCell>
                )}
                <TableCell>
                  <Badge variant={item.status === "published" ? "success" : "secondary"}>{item.status}</Badge>
                </TableCell>
                <TableCell>{item.version}</TableCell>
                <TableCell className="text-muted-foreground text-small">
                  {new Date(item.updated_at).toLocaleString()}
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-1">
                    <Button variant="outline" size="sm" asChild>
                      <Link to={`/content/${encodeURIComponent(type)}/${encodeURIComponent(item.id)}`}>Edit</Link>
                    </Button>
                    {canPublish && (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busyId === item.id}
                        onClick={() => togglePublish(item)}
                      >
                        {item.status === "published" ? "Unpublish" : "Publish"}
                      </Button>
                    )}
                    {canWrite && (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Delete ${item.id}`}
                        disabled={busyId === item.id}
                        onClick={() => setPendingDelete(item)}
                      >
                        <Trash2 className="text-destructive" />
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this item?</DialogTitle>
            <DialogDescription>This permanently deletes the item and its version history.</DialogDescription>
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
