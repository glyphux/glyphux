// Shared math + CSS-parsing for admin-ui's design-token contrast check.
// Kept dependency-free (no `culori`/etc.) and framework-agnostic on
// purpose: both `check-contrast.mjs` (the manually/CI-runnable report) and
// `check-contrast.test.mjs` (the vitest-asserted regression guard) import
// this same module, so there is exactly one implementation of the actual
// math to keep correct — never two copies that could silently drift.

/** oklch(L C H) -> linear-light sRGB, via the OKLab intermediate space
 * (https://bottosson.github.io/posts/oklab/#converting-from-linear-srgb-to-oklab).
 * Returns [r, g, b] in linear light (NOT gamma-encoded display values) —
 * that's what WCAG's own relative-luminance formula expects as input. */
export function oklchToLinearSrgb(L, C, Hdeg) {
  const h = (Hdeg * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);

  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.2914855480 * b;

  const l = l_ ** 3;
  const m = m_ ** 3;
  const s = s_ ** 3;

  const r = +4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s;
  const g = -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s;
  const bl = -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s;
  return [r, g, bl];
}

/** WCAG 2.x relative luminance (https://www.w3.org/TR/WCAG21/#dfn-relative-luminance),
 * applied directly to our already-linear-light r/g/b (oklch's own
 * conversion above already undoes sRGB's gamma curve, so there is no
 * second gamma step here — WCAG's formula operates on linear-light
 * values, which is exactly what `oklchToLinearSrgb` returns). Values are
 * clamped to [0, 1] first: oklch can represent colors outside the sRGB
 * gamut, and every token value used here is designed to be in-gamut, so a
 * small chroma/lightness combination landing just outside float-precision
 * range should clamp rather than produce a meaningless negative luminance. */
export function relativeLuminance([r, g, b]) {
  const clamp = (c) => Math.min(1, Math.max(0, c));
  return 0.2126 * clamp(r) + 0.7152 * clamp(g) + 0.0722 * clamp(b);
}

/** The WCAG contrast-ratio formula: (L1 + 0.05) / (L2 + 0.05), lighter
 * over darker. Takes two [L, C, H] oklch triples directly. */
export function contrastRatio(oklchA, oklchB) {
  const La = relativeLuminance(oklchToLinearSrgb(...oklchA));
  const Lb = relativeLuminance(oklchToLinearSrgb(...oklchB));
  const [lighter, darker] = La > Lb ? [La, Lb] : [Lb, La];
  return (lighter + 0.05) / (darker + 0.05);
}

const OKLCH_RE = /oklch\(\s*([\d.]+)\s+([\d.]+)\s+([\d.]+)\s*\)/;
const VAR_RE = /^var\((--[\w-]+)\)$/;
const DECL_RE = /(--[\w-]+)\s*:\s*([^;]+);/g;

/** Extracts every `--custom-property: value;` declaration from one CSS
 * block's *body* (the text between `{` and `}` — callers slice that out
 * first, e.g. via `extractBlock`). Returns a `Map<name, rawValue>` keyed
 * *without* the leading `--` (e.g. `"background"`, not `"--background"`)
 * so callers can look up/reference tokens by their bare name consistently
 * with `resolveOklch`'s `var(--x)`-unwrapping. Doesn't attempt full CSS
 * parsing (nested blocks, comments with `;` inside them, etc.) —
 * deliberately just enough to read `:root`/`.dark`'s flat custom-property
 * declarations in `src/index.css`, which is all this check needs. */
export function parseDeclarations(blockBody) {
  const decls = new Map();
  for (const m of blockBody.matchAll(DECL_RE)) {
    decls.set(m[1].slice(2), m[2].trim());
  }
  return decls;
}

/** Slices out the `{ ... }` body immediately following a given selector
 * (e.g. `:root` or `.dark`) from a full CSS source string. Brace-counting
 * rather than a regex match on `}` — `:root`'s own body contains no
 * nested braces today, but this is robust either way. Throws if the
 * selector isn't found, so a typo'd selector fails loudly instead of
 * silently checking against an empty token set. */
export function extractBlock(css, selector) {
  const start = css.indexOf(`${selector} {`);
  if (start === -1) throw new Error(`selector ${JSON.stringify(selector)} not found`);
  const braceStart = css.indexOf("{", start);
  let depth = 0;
  for (let i = braceStart; i < css.length; i++) {
    if (css[i] === "{") depth++;
    else if (css[i] === "}") {
      depth--;
      if (depth === 0) return css.slice(braceStart + 1, i);
    }
  }
  throw new Error(`unterminated block for selector ${JSON.stringify(selector)}`);
}

/** Resolves a declaration's value to a concrete `[L, C, H]` oklch triple,
 * following `var(--other-token)` indirection (semantic tokens like
 * `--primary` alias a raw `--gx-accent-500`, which is what actually holds
 * an `oklch(...)` literal) up to a fixed depth so a real cycle can't hang
 * this script. */
export function resolveOklch(decls, name, depth = 0) {
  if (depth > 10) throw new Error(`--${name} did not resolve to an oklch(...) literal within 10 indirections`);
  const raw = decls.get(name);
  if (raw === undefined) throw new Error(`no declaration found for --${name}`);

  const varMatch = raw.match(VAR_RE);
  if (varMatch) return resolveOklch(decls, varMatch[1].slice(2), depth + 1);

  const oklchMatch = raw.match(OKLCH_RE);
  if (!oklchMatch) throw new Error(`--${name}: ${JSON.stringify(raw)} is neither var(...) nor oklch(...)`);
  return [Number(oklchMatch[1]), Number(oklchMatch[2]), Number(oklchMatch[3])];
}

/** Parses `src/index.css`'s `:root` and `.dark` blocks into
 * `{ light: Map, dark: Map }` of raw declaration strings — the shared
 * starting point for both the CLI report and the vitest regression test,
 * so both are always checking the exact same source of truth (the real
 * committed CSS file), not a hand-copied snapshot of it.
 *
 * `.dark` only re-declares the *semantic* tokens that actually change
 * between themes (`--background`, `--primary`, etc.) — the raw
 * `--gx-neutral-*`/`--gx-accent-*` scale it points back into via `var(...)`
 * is declared once, in `:root`, and inherited by `.dark` the same way a
 * real browser's cascade would (`.dark` is applied alongside `:root` on
 * `<html>`, not instead of it — see `theme-context.tsx`). So `dark` here
 * is `:root`'s declarations with `.dark`'s overrides layered on top, not
 * `.dark`'s block in isolation — resolving `--background` under `dark`
 * would otherwise fail to find `--gx-neutral-950` at all. */
export function parseTokenBlocks(css) {
  const light = parseDeclarations(extractBlock(css, ":root"));
  const darkOverrides = parseDeclarations(extractBlock(css, ".dark"));
  const dark = new Map([...light, ...darkOverrides]);
  return { light, dark };
}
