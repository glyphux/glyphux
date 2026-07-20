import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Plus, ShieldAlert, Users as UsersIcon } from "lucide-react";
import { GlyphuxApiError, type User } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { ListSkeleton } from "@/components/layout/ListSkeleton";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";

const ROLES = ["admin", "editor", "viewer"];

export function UsersPage() {
  const { user: me } = useAuth();
  const canManage = allows(me?.role, "users:manage");

  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(canManage);
  const [error, setError] = useState<unknown>(undefined);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [pendingDeactivate, setPendingDeactivate] = useState<User | undefined>(undefined);
  const { toast } = useToast();

  const load = useCallback(() => {
    if (!canManage) return;
    setLoading(true);
    setError(undefined);
    client.users
      .list()
      .then(setUsers)
      .catch(setError)
      .finally(() => setLoading(false));
  }, [canManage]);

  useEffect(load, [load]);

  if (!canManage) {
    return (
      <Alert variant="warning">
        <ShieldAlert />
        <AlertDescription>You don't have permission to manage users. Ask an admin for access.</AlertDescription>
      </Alert>
    );
  }

  const onRoleChange = async (u: User, role: string) => {
    const prev = users;
    setUsers((cur) => cur.map((x) => (x.id === u.id ? { ...x, role } : x)));
    try {
      await client.users.updateRole(u.id, role);
      toast({ title: `${u.email} is now ${role}`, variant: "success" });
    } catch (err) {
      setUsers(prev);
      toast({
        title: err instanceof GlyphuxApiError ? err.message : "Could not change this user's role.",
        variant: "destructive",
      });
    }
  };

  const onToggleActive = async (u: User) => {
    try {
      if (u.active) {
        await client.users.deactivate(u.id);
        toast({ title: `Deactivated ${u.email}`, variant: "success" });
      } else {
        await client.users.reactivate(u.id);
        toast({ title: `Reactivated ${u.email}`, variant: "success" });
      }
      load();
    } catch (err) {
      toast({
        title: err instanceof GlyphuxApiError ? err.message : "Could not update this account.",
        variant: "destructive",
      });
    } finally {
      setPendingDeactivate(undefined);
    }
  };

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display">Users</h1>
          <p className="text-muted-foreground text-body mt-1">Manage who can access this Glyphux instance.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus /> New user
            </Button>
          </DialogTrigger>
          <DialogContent>
            <NewUserForm
              onCreated={() => {
                setDialogOpen(false);
                load();
              }}
            />
          </DialogContent>
        </Dialog>
      </div>

      {loading && <ListSkeleton />}
      {!loading && error !== undefined && <ErrorState error={error} onRetry={load} />}
      {!loading && !error && users.length === 0 && (
        <EmptyState icon={<UsersIcon />} title="No users yet" description="Create the first account." />
      )}
      {!loading && !error && users.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead>Status</TableHead>
              <TableHead />
            </TableRow>
          </TableHeader>
          <TableBody>
            {users.map((u) => {
              const isSelf = u.id === me?.id;
              return (
                <TableRow key={u.id}>
                  <TableCell className="font-medium">{u.email}</TableCell>
                  <TableCell>
                    <Select value={u.role} onValueChange={(role) => onRoleChange(u, role)} disabled={isSelf}>
                      <SelectTrigger aria-label={`Role for ${u.email}`} className="w-28 capitalize">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {ROLES.map((r) => (
                          <SelectItem key={r} value={r} className="capitalize">
                            {r}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Badge variant={u.active ? "secondary" : "destructive"}>
                      {u.active ? "Active" : "Deactivated"}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    {u.active ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-destructive"
                        disabled={isSelf}
                        onClick={() => setPendingDeactivate(u)}
                      >
                        Deactivate
                      </Button>
                    ) : (
                      <Button variant="ghost" size="sm" onClick={() => onToggleActive(u)}>
                        Reactivate
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      )}

      <Dialog open={!!pendingDeactivate} onOpenChange={(open) => !open && setPendingDeactivate(undefined)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Deactivate {pendingDeactivate?.email}?</DialogTitle>
            <DialogDescription>
              This revokes every session the account currently holds and blocks it from logging in again until an
              admin reactivates it.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPendingDeactivate(undefined)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => pendingDeactivate && onToggleActive(pendingDeactivate)}>
              Deactivate
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function NewUserForm({ onCreated }: { onCreated: () => void }) {
  const { toast } = useToast();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("editor");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(undefined);
    setSubmitting(true);
    try {
      await client.users.create(email, password, role);
      toast({ title: `Created ${email}`, variant: "success" });
      onCreated();
    } catch (err) {
      setError(err instanceof GlyphuxApiError ? err.message : "Could not create this user.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
      <DialogHeader>
        <DialogTitle>New user</DialogTitle>
        <DialogDescription>Create an account with one of Glyphux's fixed roles.</DialogDescription>
      </DialogHeader>
      {error && (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="new-user-email">Email</Label>
        <Input id="new-user-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="new-user-password">Password</Label>
        <Input
          id="new-user-password"
          type="password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="new-user-role">Role</Label>
        <Select value={role} onValueChange={setRole}>
          <SelectTrigger id="new-user-role">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ROLES.map((r) => (
              <SelectItem key={r} value={r} className="capitalize">
                {r}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button type="submit" disabled={submitting}>
        {submitting ? "Creating…" : "Create user"}
      </Button>
    </form>
  );
}
