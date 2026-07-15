import { AlertTriangle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { GlyphuxApiError } from "@glyphux/sdk";

/** A real error state for any list/detail fetch/mutation that failed (PRD
 * §5.7 quality floor). Surfaces the server's own message/validation issues
 * when available (GlyphuxApiError), rather than a generic "Something went
 * wrong". */
export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const message = error instanceof GlyphuxApiError ? error.message : error instanceof Error ? error.message : "Something went wrong.";
  const issues = error instanceof GlyphuxApiError ? error.issues : undefined;

  return (
    <Alert variant="destructive">
      <AlertTriangle />
      <AlertTitle>Couldn't load this</AlertTitle>
      <AlertDescription>
        <p>{message}</p>
        {issues && issues.length > 0 && (
          <ul className="mt-2 list-inside list-disc">
            {issues.map((issue) => (
              <li key={issue}>{issue}</li>
            ))}
          </ul>
        )}
        {onRetry && (
          <Button variant="outline" size="sm" className="mt-3" onClick={onRetry}>
            Try again
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}
