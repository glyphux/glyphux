import { describe, expect, it } from "vitest";
import { GlyphuxClient } from "../src/client.js";
import { GlyphuxApiError } from "../src/errors.js";
import { testEnv } from "./testenv.js";

async function adminClient(): Promise<GlyphuxClient> {
  const env = testEnv();
  const client = new GlyphuxClient({ baseUrl: env.baseUrl });
  await client.auth.login(env.adminEmail, env.adminPassword);
  return client;
}

// cmd/glyphuxd does not wire WithAI in yet (Ticket P4.8's tracking doc: no
// operator-facing AI provider credential configuration exists in
// internal/config today), so the real running daemon this integration suite
// talks to always 404s this route — exactly like every other opt-in
// transport option (OAuth, layouts, presets) before it's configured. The
// endpoint's actual compose-and-validate behavior is covered hermetically
// (a fake in-process Adapter, no live provider) by internal/api's own Go
// test suite (internal/api/ai_test.go), per this ticket's testing
// discipline — this test only proves the client-side request shape reaches
// the real binary and gets the documented "opt-in, not configured yet" 404,
// not a 400/401/routing bug.
describe("ai", () => {
  it("compose() 404s against the real daemon until an AI provider is configured", async () => {
    const client = await adminClient();

    let caught: unknown;
    try {
      await client.ai.compose({ prompt: "a friendly hero section", route: `ai-compose-${Date.now()}`, model: "fake-model" });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(GlyphuxApiError);
    expect((caught as GlyphuxApiError).status).toBe(404);
  });
});
