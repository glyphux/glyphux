import { useState } from "react";
import { Link } from "react-router-dom";
import { Plus, Shapes, Trash2 } from "lucide-react";
import { GlyphuxApiError } from "@glyphux/sdk";
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

export function ContentTypesListPage() {
  const { types, loading, error, reload } = useContentTypes();
  const { user } = useAuth();
  const { toast } = useToast();
  const [pendingDelete, setPendingDelete] = useState<string | undefined>(undefined);
  const [deleting, setDeleting] = useState(false);

  const canManage = allows(user?.role, "content_types:manage");
  const names = Object.keys(types).sort();

  const confirmDelete = async () => {
    if (!pendingDelete) return;
    setDeleting(true);
    try {
      await client.contentTypes.delete(pendingDelete);
      toast({ title: `Deleted "${pendingDelete}"`, variant: "success" });
      setPendingDelete(undefined);
      reload();
    } catch (err) {
      toast({
        title: "Couldn't delete this content type",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display">Content types</h1>
          <p className="text-muted-foreground text-body mt-1">Define the schema content items validate against.</p>
        </div>
        {canManage && (
          <Button asChild>
            <Link to="/content-types/new">
              <Plus /> New content type
            </Link>
          </Button>
        )}
      </div>

      {loading && <ListSkeleton />}
      {!loading && error !== undefined && <ErrorState error={error} onRetry={reload} />}
      {!loading && !error && names.length === 0 && (
        <EmptyState
          icon={<Shapes />}
          title="No content types yet"
          description="Create your first content type to start modeling content."
          action={
            canManage && (
              <Button asChild>
                <Link to="/content-types/new">
                  <Plus /> New content type
                </Link>
              </Button>
            )
          }
        />
      )}
      {!loading && !error && names.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Fields</TableHead>
              <TableHead className="w-0" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {names.map((name) => (
              <TableRow key={name}>
                <TableCell className="font-medium">
                  {canManage ? (
                    <Link to={`/content-types/${encodeURIComponent(name)}/edit`} className="hover:underline">
                      {name}
                    </Link>
                  ) : (
                    name
                  )}
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {Object.entries(types[name].fields).map(([fieldName, field]) => (
                      <Badge key={fieldName} variant="secondary">
                        {fieldName}: {field.type}
                        {field.required ? "*" : ""}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell>
                  {canManage && (
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Delete ${name}`}
                      onClick={() => setPendingDelete(name)}
                    >
                      <Trash2 className="text-destructive" />
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      <Dialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete "{pendingDelete}"?</DialogTitle>
            <DialogDescription>
              This cannot be undone. Deleting a content type that still has items will fail — remove its content
              first.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDelete(undefined)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmDelete} disabled={deleting}>
              {deleting ? "Deleting…" : "Delete"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
