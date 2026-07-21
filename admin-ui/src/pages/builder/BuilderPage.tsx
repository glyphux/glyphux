import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Editor, Frame, useEditor } from "@craftjs/core";
import { GlyphuxApiError, type BlockDefinition, type CompositionPreset, type Layout, type LayoutBlock } from "@glyphux/sdk";
import { client } from "@/lib/client";
import { useAuth } from "@/lib/auth-context";
import { allows } from "@/lib/permissions";
import { useToast } from "@/lib/toast-context";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { ErrorState } from "@/components/layout/ErrorState";
import { resolver } from "./nodes";
import { AIComposePanel } from "./AIComposePanel";
import { LivePreview } from "./LivePreview";
import { Palette } from "./Palette";
import { PropsEditor } from "./PropsEditor";
import { TreeView } from "./TreeView";
import { BlockRegistryProvider } from "./registry-context";
import { emptyLayout, layoutToNodeTree, nodeTreeToLayout } from "./serialize";

const DEFAULT_ROUTE = "home";

/** The visual builder page (Ticket P4.4b, extended by P4.5): loads the
 * block registry and an existing Layout document for a route (or starts
 * empty), and lets the user arrange blocks into regions/slots, edit their
 * props, and save back through sdk-js's public LayoutsResource — no
 * privileged access, exactly like every other admin-ui page. Two views of
 * the current draft are shown side by side for a layouts:manage-holding
 * user: TreeView, a plain structural read-out (what's placed where, no
 * network round trip), and LivePreview, a real theme-accurate render of
 * the same draft (rendered server-side through the actual themes/starter
 * theme via LayoutsResource.preview, debounced) — see LivePreview.tsx's own
 * doc comment for why the latter is not a client-side reimplementation of
 * block-to-HTML rendering. */
export function BuilderPage() {
  const params = useParams<{ "*": string }>();
  const route = params["*"] && params["*"].length > 0 ? params["*"] : DEFAULT_ROUTE;
  const { user } = useAuth();
  const canManage = allows(user?.role, "layouts:manage");

  const [blocks, setBlocks] = useState<BlockDefinition[]>([]);
  const [layout, setLayout] = useState<Layout | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>(undefined);

  const load = useCallback(() => {
    setLoading(true);
    setError(undefined);
    Promise.all([
      client.blocks.list(),
      client.layouts.get(route).catch((err) => {
        if (err instanceof GlyphuxApiError && err.status === 404) return emptyLayout();
        throw err;
      }),
    ])
      .then(([blockDefs, loadedLayout]) => {
        setBlocks(blockDefs);
        setLayout(loadedLayout);
      })
      .catch(setError)
      .finally(() => setLoading(false));
  }, [route]);

  useEffect(load, [load]);

  // Rebuilt only when the route (and therefore the loaded layout/registry)
  // changes — Frame only reads `data` on mount, so re-keying Frame by route
  // (below) is what makes switching routes actually re-hydrate the canvas
  // rather than keeping the previous route's tree.
  const initialNodes = useMemo(() => (layout ? layoutToNodeTree(layout, blocks) : undefined), [layout, blocks]);

  if (loading) {
    return (
      <div className="flex justify-center py-16">
        <Spinner label="Loading builder" />
      </div>
    );
  }

  if (error) {
    return <ErrorState error={error} onRetry={load} />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-display">Visual builder</h1>
          <p className="text-muted-foreground text-body mt-1">Arrange blocks for a route's page layout.</p>
        </div>
        <RouteSwitcher currentRoute={route} />
      </div>

      {!canManage && (
        <p className="text-muted-foreground text-small">
          You can view this layout but don't have permission to save changes or render a live preview.
        </p>
      )}

      <Editor resolver={resolver} key={route}>
        <BlockRegistryProvider registry={blocks}>
          <div className="grid grid-cols-1 gap-6 lg:grid-cols-[280px_1fr_320px]">
            <Palette blocks={blocks} />
            <Frame data={initialNodes} />
            <div className="flex flex-col gap-6">
              <PropsEditor />
              <TreeView />
            </div>
          </div>
          {/* Live preview requires layouts:manage — POST /api/v0/layouts/preview
              is gated identically to save() (see LayoutsResource.preview's
              own doc comment), so a non-manage viewer would just get a 403
              on every debounced request. Skipping the request entirely and
              explaining why (above) matches this repo's "no silently-broken
              affordance" convention rather than showing a perpetual error
              state. */}
          {canManage && <LivePreview />}
          {canManage && (
            <div className="flex flex-col gap-3">
              <SaveBar route={route} />
              <SavePresetBar />
            </div>
          )}
          {/* AI compose (Ticket P4.8) requires the same layouts:manage +
              presets:manage gate POST /api/v0/ai/compose enforces server-side
              (internal/api/ai.go's own doc comment) — a non-manage viewer
              would just get a 403, so this is hidden entirely rather than
              shown broken, mirroring LivePreview's identical reasoning
              above. onAccepted reloads this route's Layout from the server
              (load, already defined above) — accepting persists directly via
              PresetsResource.save()/import(), so the in-editor draft must be
              refreshed from storage to reflect it; going through the
              existing loading-spinner gate remounts the Editor/Frame tree
              with the freshly reloaded data (Frame only reads its `data` prop
              on mount, per LivePreview.tsx's own note). */}
          {canManage && <AIComposePanel route={route} onAccepted={load} />}
        </BlockRegistryProvider>
      </Editor>
    </div>
  );
}

function RouteSwitcher({ currentRoute }: { currentRoute: string }) {
  const navigate = useNavigate();
  const [value, setValue] = useState(currentRoute);

  useEffect(() => setValue(currentRoute), [currentRoute]);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    const trimmed = value.trim();
    if (trimmed && trimmed !== currentRoute) {
      navigate(`/builder/${trimmed}`);
    }
  };

  return (
    <form onSubmit={onSubmit} className="flex items-end gap-2">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="builder-route">Route</Label>
        <Input id="builder-route" value={value} onChange={(e) => setValue(e.target.value)} className="w-48" />
      </div>
      <Button type="submit" variant="outline">
        Load
      </Button>
    </form>
  );
}

function SaveBar({ route }: { route: string }) {
  const { query } = useEditor();
  const { toast } = useToast();
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const draft = nodeTreeToLayout(query.getSerializedNodes());
      await client.layouts.save(route, draft);
      toast({ title: "Layout saved", variant: "success" });
    } catch (err) {
      toast({
        title: "Couldn't save this layout",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex justify-end">
      <Button onClick={() => void handleSave()} disabled={saving}>
        {saving ? "Saving…" : `Save "${route}"`}
      </Button>
    </div>
  );
}

/** Builds a CompositionPreset's Manifest from draft's own composition tree:
 * every distinct block type actually used (manifest.blocks) and every
 * top-level region name actually used (manifest.slots) — the same
 * "declare what the tree references" the compatibility contract checks
 * (pkg/compat, PRD §13.3) at save/import time server-side. Kept a plain
 * function (not a hook) since it's pure and only ever called at save time. */
function manifestFor(draft: Layout): CompositionPreset["manifest"] {
  const blocks = new Set<string>();
  const slots = new Set<string>();

  const walk = (nodes: LayoutBlock[]) => {
    for (const b of nodes) {
      blocks.add(b.type);
      for (const nested of Object.values(b.slots ?? {})) walk(nested);
    }
  };
  for (const [name, region] of Object.entries(draft.regions)) {
    slots.add(name);
    walk(region.blocks);
  }

  return {
    requires_contract: draft.contract_version,
    blocks: Array.from(blocks),
    slots: Array.from(slots),
  };
}

/** "Save as preset" (Ticket P4.6): a minimal, real integration point for
 * Composition Presets in the visual builder — save the current draft as a
 * reusable, importable fragment via sdk-js's PresetsResource, prompting
 * only for the preset's own name (an inline form, mirroring RouteSwitcher's
 * identical small-form pattern, rather than a native window.prompt, which
 * doesn't play well with this codebase's React-Testing-Library seam).
 * Importing a saved preset back into a route, and a dedicated presets
 * management page (browse/delete/import-into-any-route), are left for a
 * follow-up — see this ticket's tracking doc for the scope call. */
function SavePresetBar() {
  const { query } = useEditor();
  const { toast } = useToast();
  const [name, setName] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSave = async (e: FormEvent) => {
    e.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    setSaving(true);
    try {
      const draft = nodeTreeToLayout(query.getSerializedNodes());
      const preset: CompositionPreset = {
        contract_version: "composition-preset/v1",
        name: trimmed,
        layout: draft,
        manifest: manifestFor(draft),
      };
      await client.presets.save(preset);
      toast({ title: `Preset "${trimmed}" saved`, variant: "success" });
      setName("");
    } catch (err) {
      toast({
        title: "Couldn't save this preset",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={(e) => void handleSave(e)} className="flex items-end justify-end gap-2">
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="preset-name">Preset name</Label>
        <Input
          id="preset-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="e.g. hero-section"
          className="w-56"
        />
      </div>
      <Button type="submit" variant="outline" disabled={saving || !name.trim()}>
        {saving ? "Saving…" : "Save as preset"}
      </Button>
    </form>
  );
}
