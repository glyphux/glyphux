#!/usr/bin/env node
// Verifies WCAG AA color contrast for admin-ui's design tokens — the
// "verified, not assumed" accessibility-floor requirement from ticket DS
// (docs/specs/phase3-4-spec.md). Reads the *real* `:root`/`.dark` custom
// properties straight out of src/index.css (no hand-copied token values
// to drift out of sync) and reports every pair's actual contrast ratio
// against its WCAG threshold, for both themes.
//
// Run manually with:
//   cd admin-ui && npm run check:contrast
//
// Also asserted as a regression guard in `check-contrast.test.mjs` (run by
// `npm test`), so a future token change that regresses contrast fails CI,
// not just a manual read of this script's output.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { contrastRatio, parseTokenBlocks, resolveOklch } from "./contrast-lib.mjs";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const cssPath = path.join(__dirname, "../src/index.css");
const css = readFileSync(cssPath, "utf8");
const tokens = parseTokenBlocks(css);

// [tokenA, tokenB, threshold, label] — thresholds per WCAG 2.1 AA:
// 4.5:1 for normal text (SC 1.4.3), 3:1 for large text and for
// "visual information required to identify UI components" (SC 1.4.11).
const CHECKS = [
  ["background", "foreground", 4.5, "body text on background"],
  ["card", "card-foreground", 4.5, "body text on card"],
  ["primary", "primary-foreground", 4.5, "button text on primary bg"],
  ["secondary", "secondary-foreground", 4.5, "button text on secondary bg"],
  ["muted", "muted-foreground", 4.5, "muted/caption text on muted bg"],
  ["destructive", "destructive-foreground", 4.5, "button text on destructive bg"],
  ["background", "muted-foreground", 4.5, "muted text directly on background (worst case placement)"],
  ["background", "ring", 3, "focus ring vs background (UI component boundary)"],
  // Known, documented gap — see docs/implementation/active/0020-design-system.md
  // ("Current Decisions" / "Risks"). Listed here (not silently omitted) so
  // the report always shows its real, current number; `allowFail: true`
  // keeps it from failing this script's exit code, since fixing it means a
  // global border-color change tracked as separate follow-up work, not
  // something to silently "pass" by omission.
  ["background", "border", 3, "border vs background (UI component boundary)", { allowFail: true }],
];

let anyRealFailure = false;

for (const mode of ["light", "dark"]) {
  console.log(`\n=== ${mode} ===`);
  const decls = tokens[mode];
  for (const [a, b, threshold, label, opts] of CHECKS) {
    const ratio = contrastRatio(resolveOklch(decls, a), resolveOklch(decls, b));
    const passed = ratio >= threshold;
    const allowFail = opts?.allowFail ?? false;
    const tag = passed ? "PASS" : allowFail ? "KNOWN GAP" : "FAIL";
    console.log(`${tag.padEnd(9)} ${ratio.toFixed(2)}:1  (needs >= ${threshold}:1)  ${label}`);
    if (!passed && !allowFail) anyRealFailure = true;
  }
}

if (anyRealFailure) {
  console.error("\nOne or more token pairs are below their required WCAG AA contrast ratio.");
  process.exit(1);
}
console.log("\nAll checked pairs meet their WCAG AA threshold (known, documented gaps excepted).");
