import { useCallback, useEffect, useMemo, useState } from "react";
import { CheckCircle2, Plug, ShieldAlert, ShieldCheck, ShieldX } from "lucide-react";
import { GlyphuxApiError, type ConsentPermission, type ConsentScope, type PluginConsentStatus } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { ListSkeleton } from "@/components/layout/ListSkeleton";

/** A selected-grant key: "capability:scope" for an API scope, "perm:<name>"
 * for a permission. Uniquely identifies one checkbox. */
type GrantKey = string;

const scopeKey = (s: ConsentScope): GrantKey[] => s.scopes.map((sc) => `${s.capability}:${sc}`);
const permissionKey = (p: ConsentPermission): GrantKey => `perm:${p.name}`;

/** ConsentScreen (Ticket T4 / gap 2): the admin's install-time consent
 * surface. Pending requests (undecided, explicitly denied, or with a stale
 * fingerprint — i.e. every registered plugin without a live decision) get
 * an Approve / Deny / partial-selection UI; the plugin table below shows
 * every registered plugin's current status and, for live decisions, the
 * exact granted subset. Admin-only (plugins:manage). */
export function PluginsPage() {
  const { user: me } = useAuth();
  const canManage = allows(me?.role, "plugins:manage");

  const [plugins, setPlugins] = useState<PluginConsentStatus[]>([]);
  const [pendingNames, setPendingNames] = useState<string[]>([]);
  const [loading, setLoading] = useState(canManage);
  const [error, setError] = useState<unknown>(undefined);
  // Per-plugin checkbox selection, defaulted to the full request.
  const [selection, setSelection] = useState<Record<string, Set<GrantKey>>>({});
  const [submitting, setSubmitting] = useState<string | undefined>(undefined);
  const { toast } = useToast();

  const load = useCallback(() => {
    if (!canManage) return;
    setLoading(true);
    setError(undefined);
    Promise.all([client.plugins.list(), client.plugins.consentRequests()])
      .then(([list, requests]) => {
        setPlugins(list);
        const pending = requests.map((r) => r.name);
        setPendingNames(pending);
        // Default every pending request's selection to its full request so
        // "Approve" needs no interaction and unchecking is opt-out.
        setSelection((cur) => {
          const next = { ...cur };
          for (const r of requests) {
            const keys = new Set<GrantKey>();
            for (const s of r.api) scopeKey(s).forEach((k) => keys.add(k));
            for (const p of r.permissions) keys.add(permissionKey(p));
            next[r.name] = keys;
          }
          return next;
        });
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, [canManage]);

  useEffect(load, [load]);

  const pendingRequests = useMemo(
    () => plugins.filter((p) => pendingNames.includes(p.name)),
    [plugins, pendingNames],
  );

  if (!canManage) {
    return (
      <Alert variant="warning">
        <ShieldAlert />
        <AlertDescription>You don't have permission to manage plugins. Ask an admin for access.</AlertDescription>
      </Alert>
    );
  }

  const toggleKey = (plugin: string, key: GrantKey) => {
    setSelection((cur) => {
      const next = new Set(cur[plugin] ?? []);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return { ...cur, [plugin]: next };
    });
  };

  /** Submits a decision for plugin from the current checkbox selection.
   * All boxes checked → approved (the full request); none → denied; a
   * middle subset → partial. */
  const submit = async (plugin: string, granted: { api: ConsentScope[]; permissions: ConsentPermission[] }) => {
    const req = plugins.find((p) => p.name === plugin)!.requested;
    const full =
      granted.api.length === req.api.length &&
      granted.permissions.length === req.permissions.length;
    setSubmitting(plugin);
    try {
      await client.plugins.decide(plugin, {
        decision: granted.api.length === 0 && granted.permissions.length === 0 ? "denied" : full ? "approved" : "partial",
        ...(granted.api.length > 0 || granted.permissions.length > 0
          ? { granted_api: granted.api, granted_permissions: granted.permissions }
          : {}),
      });
      toast({ title: full ? `Approved ${plugin}` : `Recorded decision for ${plugin}`, variant: "success" });
      await load();
    } catch (err) {
      toast({
        title: err instanceof GlyphuxApiError ? err.message : `Could not record the decision for ${plugin}.`,
        variant: "destructive",
      });
    } finally {
      setSubmitting(undefined);
    }
  };

  const selectionGrant = (p: PluginConsentStatus): { api: ConsentScope[]; permissions: ConsentPermission[] } => {
    const sel = selection[p.name] ?? new Set<GrantKey>();
    return {
      api: p.requested.api
        .map((s) => ({ ...s, scopes: s.scopes.filter((sc) => sel.has(`${s.capability}:${sc}`)) }))
        .filter((s) => s.scopes.length > 0),
      permissions: p.requested.permissions.filter((pm) => sel.has(permissionKey(pm))),
    };
  };

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-display">Plugins</h1>
        <p className="text-muted-foreground text-body mt-1">
          Approve the permissions installed capabilities request, or grant a subset.
        </p>
      </div>

      {loading && <ListSkeleton />}
      {!loading && error !== undefined && <ErrorState error={error} onRetry={load} />}
      {!loading && !error && pendingRequests.length === 0 && (
        <EmptyState
          icon={<ShieldCheck />}
          title="No pending consent requests"
          description="Every installed plugin has a live decision. A plugin reappears here if its requested permissions change."
        />
      )}
      {!loading && !error &&
        pendingRequests.map((p) => (
          <section key={p.name} className="bg-card border-border flex flex-col gap-4 rounded-lg border p-5" aria-label={`Consent for ${p.name}`}>
            <div className="flex items-start justify-between gap-4">
              <div>
                <h2 className="text-h3 font-semibold">{p.name}</h2>
                <p className="text-muted-foreground text-small">
                  v{p.version} · fingerprint {p.fingerprint.slice(0, 12)}…
                  {p.status === "denied" && " · previously denied — needs re-consent"}
                </p>
              </div>
              <Badge variant={p.status === "denied" ? "destructive" : "secondary"}>
                {p.status === "denied" ? "Denied" : "Pending"}
              </Badge>
            </div>

            <fieldset className="flex flex-col gap-3">
              <legend className="text-muted-foreground text-small font-medium">Requested permissions</legend>
              {p.requested.api.map((s) => (
                <div key={s.capability} className="flex flex-col gap-1.5">
                  <span className="text-body font-medium">{s.capability}</span>
                  <div className="flex flex-wrap gap-2">
                    {s.scopes.map((sc) => {
                      const key = `${s.capability}:${sc}`;
                      return (
                        <Label
                          key={key}
                          className="bg-muted text-muted-foreground flex items-center gap-2 rounded-md border px-3 py-1.5 text-small"
                        >
                          <Checkbox
                            checked={selection[p.name]?.has(key) ?? true}
                            onCheckedChange={() => toggleKey(p.name, key)}
                          />
                          {sc}
                        </Label>
                      );
                    })}
                  </div>
                </div>
              ))}
              {p.requested.permissions.length > 0 && (
                <div className="flex flex-wrap gap-2">
                  {p.requested.permissions.map((pm) => {
                    const key = permissionKey(pm);
                    return (
                      <Label
                        key={key}
                        className="bg-muted text-muted-foreground flex items-center gap-2 rounded-md border px-3 py-1.5 text-small"
                      >
                        <Checkbox
                          checked={selection[p.name]?.has(key) ?? true}
                          onCheckedChange={() => toggleKey(p.name, key)}
                        />
                        {pm.name}
                      </Label>
                    );
                  })}
                </div>
              )}
            </fieldset>

            <div className="flex flex-wrap items-center gap-2">
              <Button size="sm" disabled={submitting === p.name} onClick={() => submit(p.name, selectionGrant(p))}>
                <CheckCircle2 /> Apply selection
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={submitting === p.name}
                onClick={() =>
                  submit(p.name, {
                    api: p.requested.api.map((s) => ({ ...s, scopes: [...s.scopes] })),
                    permissions: p.requested.permissions.map((pm) => ({ ...pm })),
                  })
                }
              >
                <ShieldCheck /> Approve all
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="text-destructive"
                disabled={submitting === p.name}
                onClick={() => submit(p.name, { api: [], permissions: [] })}
              >
                <ShieldX /> Deny
              </Button>
            </div>
          </section>
        ))}

      {!loading && !error && plugins.length > 0 && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Plugin</TableHead>
              <TableHead>Version</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Granted</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {plugins.map((p) => (
              <TableRow key={p.name}>
                <TableCell className="font-medium">
                  <span className="flex items-center gap-2">
                    <Plug className="size-4 text-muted-foreground" /> {p.name}
                  </span>
                </TableCell>
                <TableCell>{p.version}</TableCell>
                <TableCell>
                  <Badge variant={p.status === "denied" ? "destructive" : p.status === "undecided" ? "secondary" : "default"}>
                    {p.status === "approved" ? "Approved" : p.status === "partial" ? "Partial" : p.status === "denied" ? "Denied" : "Not asked"}
                  </Badge>
                </TableCell>
                <TableCell className="text-muted-foreground text-small">
                  {p.granted
                    ? [
                        ...p.granted.api.flatMap((s) => s.scopes.map((sc) => `${s.capability}:${sc}`)),
                        ...p.granted.permissions.map((pm) => pm.name),
                      ].join(", ")
                    : "—"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}

      {!loading && !error && pendingRequests.length === 0 && plugins.length === 0 && (
        <EmptyState icon={<Plug />} title="No plugins" description="No capability plugins are registered." />
      )}
    </div>
  );
}
