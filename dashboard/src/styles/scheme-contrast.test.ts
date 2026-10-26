import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { colorSchemes } from "@/lib/color-scheme";

/*
 * Every colour scheme, light and dark, read the way the browser reads it:
 * Tailwind's palette, then coss's tokens, then the schemes' overrides, with
 * `var()`, `color-mix(in srgb, …)` and `--alpha()` resolved and translucent
 * surfaces laid over what they sit on. Each pair a reader depends on must
 * clear its WCAG 2 ratio.
 */

type Rgba = { r: number; g: number; b: number; a: number };
type Mode = "light" | "dark";

const read = (path: string) =>
  readFileSync(fileURLToPath(new URL(path, import.meta.url)), "utf8");
const require = createRequire(import.meta.url);
const tailwindTheme = readFileSync(
  require.resolve("tailwindcss/theme.css"),
  "utf8",
);
const coss = read("./coss.css");
const schemes = read("./schemes.css");

/** The custom properties declared in the block whose selector is exactly `selector`. */
function block(css: string, selector: string): Map<string, string> {
  const out = new Map<string, string>();
  let from = 0;
  for (;;) {
    const at = css.indexOf(`${selector} {`, from);
    if (at < 0) return out;
    // The selector must start a rule, not end a longer one.
    const before = css.slice(0, at).trimEnd().at(-1);
    from = at + selector.length;
    if (
      before !== undefined &&
      before !== "}" &&
      before !== "/" &&
      before !== ";"
    )
      continue;
    let depth = 0;
    let end = at;
    for (let i = css.indexOf("{", at); i < css.length; i++) {
      if (css[i] === "{") depth++;
      if (css[i] === "}" && --depth === 0) {
        end = i;
        break;
      }
    }
    const body = css.slice(css.indexOf("{", at) + 1, end);
    for (const declaration of splitTopLevel(body, ";")) {
      const match = declaration.trim().match(/^(--[\w-]+)\s*:\s*([\s\S]+)$/);
      if (match) out.set(match[1] as string, (match[2] as string).trim());
    }
  }
}

function splitTopLevel(text: string, separator: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let current = "";
  for (const ch of text) {
    if (ch === "(") depth++;
    if (ch === ")") depth--;
    if (ch === separator && depth === 0) {
      parts.push(current);
      current = "";
    } else current += ch;
  }
  if (current.trim()) parts.push(current);
  return parts;
}

function tokensFor(scheme: string, mode: Mode): Map<string, string> {
  const layers = [
    block(tailwindTheme, "@theme default"),
    block(coss, ":root"),
    ...(mode === "dark" ? [block(coss, ".dark"), block(schemes, ".dark")] : []),
    ...(mode === "light" ? [block(schemes, ":root:not(.dark)")] : []),
    block(
      schemes,
      `:root[data-scheme="${scheme}"]${mode === "light" ? ":not(.dark)" : ".dark"}`,
    ),
  ];
  const tokens = new Map<string, string>();
  for (const layer of layers) for (const [k, v] of layer) tokens.set(k, v);
  return tokens;
}

function oklchToRgb(l: number, c: number, h: number): Rgba {
  const hr = (h * Math.PI) / 180;
  const a = c * Math.cos(hr);
  const b = c * Math.sin(hr);
  const l_ = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m_ = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s_ = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [
    4.0767416621 * l_ - 3.3077115913 * m_ + 0.2309699292 * s_,
    -1.2684380046 * l_ + 2.6097574011 * m_ - 0.3413193965 * s_,
    -0.0041960863 * l_ - 0.7034186147 * m_ + 1.707614701 * s_,
  ].map((x) => {
    const v = Math.min(1, Math.max(0, x));
    return v <= 0.0031308 ? 12.92 * v : 1.055 * v ** (1 / 2.4) - 0.055;
  });
  return {
    r: lin[0] as number,
    g: lin[1] as number,
    b: lin[2] as number,
    a: 1,
  };
}

function resolve(value: string, tokens: Map<string, string>, depth = 0): Rgba {
  if (depth > 20) throw new Error(`cycle resolving ${value}`);
  const v = value.trim();
  if (v === "transparent") return { r: 0, g: 0, b: 0, a: 0 };
  const hex = v.match(/^#([0-9a-f]{6})$/i);
  if (hex) {
    const n = Number.parseInt(hex[1] as string, 16);
    return {
      r: ((n >> 16) & 255) / 255,
      g: ((n >> 8) & 255) / 255,
      b: (n & 255) / 255,
      a: 1,
    };
  }
  if (v === "#000") return { r: 0, g: 0, b: 0, a: 1 };
  if (v === "#fff") return { r: 1, g: 1, b: 1, a: 1 };
  const ok = v.match(
    /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+|none)\s+([\d.]+|none)\s*\)$/,
  );
  if (ok) {
    const l = Number(ok[1]) / (ok[2] === "%" ? 100 : 1);
    const c = ok[3] === "none" ? 0 : Number(ok[3]);
    const h = ok[4] === "none" ? 0 : Number(ok[4]);
    return oklchToRgb(l, c, h);
  }
  const ref = v.match(/^var\(\s*(--[\w-]+)\s*\)$/);
  if (ref) {
    const next = tokens.get(ref[1] as string);
    if (next === undefined) throw new Error(`unknown token ${ref[1]}`);
    return resolve(next, tokens, depth + 1);
  }
  const alpha = v.match(/^--alpha\(\s*([\s\S]+?)\s*\/\s*([\d.]+)%\s*\)$/);
  if (alpha) {
    const base = resolve(alpha[1] as string, tokens, depth + 1);
    return { ...base, a: base.a * (Number(alpha[2]) / 100) };
  }
  const mix = v.match(/^color-mix\(\s*in srgb\s*,([\s\S]+)\)$/);
  if (mix) {
    const [first, second] = splitTopLevel(mix[1] as string, ",").map((part) => {
      const m = part.trim().match(/^([\s\S]+?)(?:\s+([\d.]+)%)?$/);
      return {
        color: resolve(m?.[1] as string, tokens, depth + 1),
        p: m?.[2] === undefined ? undefined : Number(m[2]) / 100,
      };
    });
    if (!first || !second) throw new Error(`bad color-mix ${v}`);
    const p1 = first.p ?? (second.p === undefined ? 0.5 : 1 - second.p);
    const p2 = second.p ?? 1 - p1;
    const a = first.color.a * p1 + second.color.a * p2;
    const ch = (k: "r" | "g" | "b") =>
      a === 0
        ? 0
        : (first.color[k] * first.color.a * p1 +
            second.color[k] * second.color.a * p2) /
          a;
    return { r: ch("r"), g: ch("g"), b: ch("b"), a };
  }
  throw new Error(`cannot resolve ${v}`);
}

/** `top` laid over the opaque `bottom`. */
function over(top: Rgba, bottom: Rgba): Rgba {
  const mixCh = (k: "r" | "g" | "b") =>
    top[k] * top.a + bottom[k] * (1 - top.a);
  return { r: mixCh("r"), g: mixCh("g"), b: mixCh("b"), a: 1 };
}

function luminance({ r, g, b }: Rgba) {
  const lin = (c: number) =>
    c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function ratio(a: Rgba, b: Rgba) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [
    number,
    number,
  ];
  return (hi + 0.05) / (lo + 0.05);
}

/** Text or a mark (`fg`) on a surface (`on`), each over the surfaces under it. */
type Pair = { fg: string; on: string[]; min: number };

const pairs: Record<string, Pair> = {
  "body text": { fg: "foreground", on: ["background"], min: 4.5 },
  "text on a card": {
    fg: "card-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
  "text in a popover": {
    fg: "popover-foreground",
    on: ["background", "popover"],
    min: 4.5,
  },
  "muted text": { fg: "muted-foreground", on: ["background"], min: 4.5 },
  "muted text on a card": {
    fg: "muted-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
  "muted text in a well": {
    fg: "muted-foreground",
    on: ["background", "muted"],
    min: 4.5,
  },
  "sidebar text": { fg: "sidebar-foreground", on: ["sidebar"], min: 7 },
  "selected sidebar item": {
    fg: "sidebar-accent-foreground",
    on: ["sidebar", "sidebar-accent"],
    min: 4.5,
  },
  "primary button": {
    fg: "primary-foreground",
    on: ["background", "primary"],
    min: 4.5,
  },
  "secondary button": {
    fg: "secondary-foreground",
    on: ["background", "secondary"],
    min: 4.5,
  },
  "hovered item": {
    fg: "accent-foreground",
    on: ["background", "accent"],
    min: 4.5,
  },
  "focus ring": { fg: "ring", on: ["background"], min: 3 },
  "error text": { fg: "destructive-foreground", on: ["background"], min: 4.5 },
  "error text on a card": {
    fg: "destructive-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
  "warning text": { fg: "warning-foreground", on: ["background"], min: 4.5 },
  "warning text on a card": {
    fg: "warning-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
  "success text": { fg: "success-foreground", on: ["background"], min: 4.5 },
  "success text on a card": {
    fg: "success-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
  "info text": { fg: "info-foreground", on: ["background"], min: 4.5 },
  "info text on a card": {
    fg: "info-foreground",
    on: ["background", "card"],
    min: 4.5,
  },
};

function contrastOf(scheme: string, mode: Mode, pair: Pair): number {
  const tokens = tokensFor(scheme, mode);
  let surface: Rgba = { r: 1, g: 1, b: 1, a: 1 };
  for (const layer of pair.on)
    surface = over(resolve(`var(--${layer})`, tokens), surface);
  return ratio(over(resolve(`var(--${pair.fg})`, tokens), surface), surface);
}

describe.each(colorSchemes.map((s) => s.slug))("the %s scheme", (scheme) => {
  describe.each(["light", "dark"] as const)("in %s mode", (mode) => {
    it.each(Object.entries(pairs))("keeps %s readable", (_label, pair) => {
      expect(contrastOf(scheme, mode, pair)).toBeGreaterThanOrEqual(pair.min);
    });
  });

  it("declares a light and a dark block", () => {
    if (scheme === "graphite") return;
    expect(
      block(schemes, `:root[data-scheme="${scheme}"]:not(.dark)`).size,
    ).toBeGreaterThan(0);
    expect(
      block(schemes, `:root[data-scheme="${scheme}"].dark`).size,
    ).toBeGreaterThan(0);
  });
});

it("resolves coss's tokens as the browser does", () => {
  // coss's light sidebar text before the adjustment measured 4.58:1, the
  // figure the design documents for it.
  const tokens = tokensFor("graphite", "light");
  tokens.set(
    "--sidebar-foreground",
    "color-mix(in srgb, var(--color-neutral-800) 64%, var(--sidebar))",
  );
  const fg = resolve("var(--sidebar-foreground)", tokens);
  const bg = resolve("var(--sidebar)", tokens);
  expect(ratio(fg, bg)).toBeCloseTo(4.58, 1);
});
