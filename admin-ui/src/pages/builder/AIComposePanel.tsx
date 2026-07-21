import { useState } from "react";
import { GlyphuxApiError, type AIComposeResult } from "@glyphux/sdk";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import { Spinner } from "@/components/ui/spinner";
import { ErrorState } from "@/components/layout/ErrorState";
import { client } from "@/lib/client";
import { useToast } from "@/lib/toast-context";

/** The model string sent to POST /api/v0/ai/compose when the operator's
 * configured provider Adapter doesn't call for anything more specific — an
 * admin-ui default only, never interpreted client-side (capabilities/ai.
 * GenerateRequest.Model is passed through opaquely all the way to whichever
 * provider Adapter this daemon was wired with; see internal/api/ai.go's own
 * doc comment on why no server-side default is guessed either). */
const DEFAULT_MODEL = "claude-3-5-sonnet-20241022";

/** "Describe what you want, get a composition fragment back" (Ticket P4.8,
 * PRD §14.1 Surface 2): a prompt box that calls POST /api/v0/ai/compose via
 * sdk-js's AiResource, previews the proposed Layer-2 fragment through the
 * SAME iframe-preview pattern LivePreview.tsx already established
 * (server-rendered HTML embedded via srcDoc, sandbox=""), and lets the user
 * accept (which reuses PresetsResource.save() then PresetsResource.import()
 * unchanged — the identical path an ordinary preset import already uses,
 * never a second insert path) or discard.
 *
 * A rejected/incompatible proposal (result.compatible === false) renders
 * the real compat diagnostics (missing_blocks/missing_slots/
 * unsupported_contract) inline — never a generic error banner, mirroring
 * how the compat.Result shape already renders elsewhere in this codebase
 * (PRD §13.3's "explainable, not a bare rejection" requirement, extended
 * here to AI output). A hard request failure (bad JSON from the model, no
 * provider configured, network error) uses ErrorState instead, since that
 * genuinely is an error, not a declined-but-explained result.
 *
 * onAccepted is called after a successful accept so the parent BuilderPage
 * can reload the route's now-updated Layout into the canvas — this panel
 * never mutates the in-editor draft directly, only the persisted Layout via
 * the existing save/import endpoints, exactly like SaveBar/SavePresetBar's
 * own separation of concerns. */
export function AIComposePanel({ route, onAccepted }: { route: string; onAccepted: () => void }) {
  const { toast } = useToast();
  const [prompt, setPrompt] = useState("");
  const [loading, setLoading] = useState(false);
  const [accepting, setAccepting] = useState(false);
  const [error, setError] = useState<unknown>(undefined);
  const [result, setResult] = useState<AIComposeResult | undefined>(undefined);

  const handleGenerate = async () => {
    const trimmed = prompt.trim();
    if (!trimmed) return;
    setLoading(true);
    setError(undefined);
    setResult(undefined);
    try {
      const composed = await client.ai.compose({ prompt: trimmed, route, model: DEFAULT_MODEL });
      setResult(composed);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  };

  const handleDiscard = () => {
    setResult(undefined);
    setError(undefined);
  };

  const handleAccept = async () => {
    if (!result?.fragment) return;
    setAccepting(true);
    try {
      const saved = await client.presets.save(result.fragment);
      const imported = await client.presets.import(saved.id, route);
      if (!imported.compatible) {
        // Compatibility can, in principle, have changed between compose()'s
        // own check and this accept step (e.g. a concurrent edit removed a
        // block) — decline exactly like an ordinary preset import would,
        // rather than pretending the merge happened.
        setResult({ ...result, ...imported, fragment: undefined });
        toast({ title: "Couldn't import this fragment", description: "It's no longer compatible.", variant: "destructive" });
        return;
      }
      toast({ title: "AI-composed fragment added", variant: "success" });
      setResult(undefined);
      setPrompt("");
      onAccepted();
    } catch (err) {
      toast({
        title: "Couldn't accept this fragment",
        description: err instanceof GlyphuxApiError ? err.message : "Something went wrong.",
        variant: "destructive",
      });
    } finally {
      setAccepting(false);
    }
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2">
        <CardTitle>AI compose</CardTitle>
        {loading && <Spinner label="Composing" />}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="ai-compose-prompt">Describe what you want</Label>
          <Textarea
            id="ai-compose-prompt"
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="e.g. a friendly hero section with a heading and a call-to-action"
            disabled={loading}
          />
        </div>
        <div className="flex justify-end">
          <Button onClick={() => void handleGenerate()} disabled={loading || !prompt.trim()}>
            {loading ? "Generating…" : "Generate"}
          </Button>
        </div>

        {error ? <ErrorState error={error} /> : null}

        {result && !result.compatible && (
          <div role="alert" className="border-destructive/50 bg-destructive/5 rounded-md border p-3">
            <p className="text-small font-medium">This proposal can't be used as-is:</p>
            <ul className="text-small mt-2 list-inside list-disc">
              {result.missing_blocks?.map((b) => (
                <li key={`block-${b}`}>block type "{b}" is not registered</li>
              ))}
              {result.missing_slots?.map((s) => (
                <li key={`slot-${s}`}>region "{s}" is not declared by the destination theme</li>
              ))}
              {result.unsupported_contract && <li>unsupported contract version "{result.unsupported_contract}"</li>}
            </ul>
            <div className="mt-3 flex justify-end">
              <Button variant="outline" size="sm" onClick={handleDiscard}>
                Discard
              </Button>
            </div>
          </div>
        )}

        {result?.compatible && result.preview && (
          <div className="flex flex-col gap-3">
            <iframe
              title="AI compose preview"
              srcDoc={result.preview.html}
              sandbox=""
              className="h-[400px] w-full rounded border bg-white"
            />
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={handleDiscard} disabled={accepting}>
                Discard
              </Button>
              <Button onClick={() => void handleAccept()} disabled={accepting}>
                {accepting ? "Adding…" : "Accept"}
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
