import type { CSSProperties } from "react";
import type { LoginSurface } from "@/api/raw-paths";

export interface SurfaceStyle {
  className: string;
  style?: CSSProperties;
}

const LIGHT_BACKGROUND = "var(--color-white)";
const DARK_BACKGROUND =
  "color-mix(in_srgb,var(--color-neutral-950)_96%,var(--color-white))";

/**
 * An opaque surface takes the theme's background. A translucent one is that
 * colour at the chosen opacity, set through one custom property inline, and is
 * written from the constants coss.css builds `--background` from, because the
 * card redefines `--background` for its own controls. Frosted glass blurs and
 * lifts what shows through.
 */
export function surfaceStyle(surface: LoginSurface): SurfaceStyle {
  if (!surface.translucent) return { className: "bg-background" };
  const fill = [
    `bg-[color-mix(in_srgb,${LIGHT_BACKGROUND}_var(--card-alpha),transparent)]`,
    `dark:bg-[color-mix(in_srgb,${DARK_BACKGROUND}_var(--card-alpha),transparent)]`,
  ];
  if (surface.blur) fill.push("backdrop-blur-xl backdrop-saturate-150");
  return {
    className: fill.join(" "),
    style: { "--card-alpha": `${surface.opacity}%` } as CSSProperties,
  };
}

/** A see-through mix of `color` at `percent` of half the card's opacity. */
function halfAlpha(color: string, percent: number): string {
  return `color-mix(in_srgb,${color}_calc(var(--card-alpha)*${percent / 200}),transparent)`;
}

/**
 * On a translucent card, the controls drawn on it read these coss tokens at half
 * the card's opacity, so the picture shows through fields and buttons. Edges and
 * hover fills keep their usual share of ink (black in light, white in dark). They
 * are redefined from constants, not from their own values. Popups portal to the
 * body and keep the opaque root tokens.
 */
const translucentControls = [
  `[--background:${halfAlpha(LIGHT_BACKGROUND, 100)}]`,
  `[--input:${halfAlpha("var(--color-black)", 10)}]`,
  `[--border:${halfAlpha("var(--color-black)", 8)}]`,
  `[--muted:${halfAlpha("var(--color-black)", 4)}]`,
  `[--accent:${halfAlpha("var(--color-black)", 4)}]`,
  `dark:[--background:${halfAlpha(DARK_BACKGROUND, 100)}]`,
  `dark:[--input:${halfAlpha("var(--color-white)", 8)}]`,
  `dark:[--border:${halfAlpha("var(--color-white)", 6)}]`,
  `dark:[--muted:${halfAlpha("var(--color-white)", 4)}]`,
  `dark:[--accent:${halfAlpha("var(--color-white)", 4)}]`,
].join(" ");

/**
 * A translucent surface that carries controls — the card, and the capsules'
 * buttons — also gives them see-through tokens, so the public page and the
 * settings preview agree.
 */
export function cardSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  const style = surfaceStyle(surface);
  return surface.translucent
    ? { ...style, className: `${style.className} ${translucentControls}` }
    : style;
}

/**
 * A capsule takes the card's fill rule with its own settings, so an opaque one
 * is the background colour and a translucent one gives its buttons the same
 * see-through tokens as the card.
 */
export function capsuleSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  return cardSurfaceStyle(surface);
}
