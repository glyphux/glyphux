import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { describe, expect, it } from "vitest";
import { contrastRatio, parseTokenBlocks, resolveOklch } from "./contrast-lib.mjs";

// Seam: the design-token contrast check as a *regression guard*, not just
// a one-time manual read of check-contrast.mjs's console output. Parses
// the real, committed src/index.css (same as the CLI script) so a future
// token edit that regresses contrast fails `npm test`, not just a report
// nobody re-runs. See docs/implementation/active/0020-design-system.md for
// the full audit this codifies.
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const css = readFileSync(path.join(__dirname, "../src/index.css"), "utf8");
const tokens = parseTokenBlocks(css);

describe("design-token color contrast (WCAG AA)", () => {
  it.each([
    ["light", "background", "foreground", 4.5],
    ["light", "card", "card-foreground", 4.5],
    ["light", "primary", "primary-foreground", 4.5],
    ["light", "secondary", "secondary-foreground", 4.5],
    ["light", "muted", "muted-foreground", 4.5],
    ["light", "destructive", "destructive-foreground", 4.5],
    ["light", "background", "muted-foreground", 4.5],
    ["light", "background", "ring", 3],
    ["dark", "background", "foreground", 4.5],
    ["dark", "card", "card-foreground", 4.5],
    ["dark", "primary", "primary-foreground", 4.5],
    ["dark", "secondary", "secondary-foreground", 4.5],
    ["dark", "muted", "muted-foreground", 4.5],
    ["dark", "destructive", "destructive-foreground", 4.5],
    ["dark", "background", "muted-foreground", 4.5],
    ["dark", "background", "ring", 3],
  ])("%s: %s/%s clears its WCAG threshold (>= %s:1)", (mode, a, b, threshold) => {
    const ratio = contrastRatio(resolveOklch(tokens[mode], a), resolveOklch(tokens[mode], b));
    expect(ratio).toBeGreaterThanOrEqual(threshold);
  });

  // The one known, *documented* gap (see the tracking doc's Current
  // Decisions/Risks): border/input contrast against background sits well
  // under WCAG 1.4.11's 3:1 non-text threshold in both themes. This is
  // intentionally asserted as "still failing, in the expected range" —
  // not silently ignored — so that if a future change accidentally
  // pushes it to pass (or further regresses it), this test forces a
  // conscious update here and in the tracking doc, rather than the gap
  // quietly drifting in either direction unnoticed.
  it.each([
    ["light", 1.0, 1.5],
    ["dark", 1.0, 1.5],
  ])("%s: border/background is a known, documented sub-3:1 gap (not yet fixed)", (mode, min, max) => {
    const ratio = contrastRatio(resolveOklch(tokens[mode], "background"), resolveOklch(tokens[mode], "border"));
    expect(ratio).toBeLessThan(3);
    expect(ratio).toBeGreaterThanOrEqual(min);
    expect(ratio).toBeLessThan(max);
  });
});
