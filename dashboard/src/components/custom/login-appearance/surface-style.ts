import type { CSSProperties } from "react";
import type { LoginSurface } from "@/api/raw-paths";

export interface SurfaceStyle {
  className: string;
  style?: CSSProperties;
}

/**
 * An opaque surface takes the theme's background. A translucent one is that
 * colour at the chosen opacity, set through one custom property inline and
 * drawn by the `surface-glass` rule in src/styles/index.css. Frosted glass
 * blurs and lifts what shows through.
 */
export function surfaceStyle(surface: LoginSurface): SurfaceStyle {
  if (!surface.translucent) return { className: "bg-background" };
  const fill = ["surface-glass"];
  if (surface.blur) fill.push("backdrop-blur-xl backdrop-saturate-150");
  return {
    className: fill.join(" "),
    style: { "--card-alpha": `${surface.opacity}%` } as CSSProperties,
  };
}

/**
 * A translucent surface that carries controls — the card, and the capsules'
 * buttons — also gives them see-through fill tokens (`surface-glass-controls`),
 * so the public page and the settings preview agree. Popups portal to the body
 * and keep the opaque root tokens.
 */
export function cardSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  const style = surfaceStyle(surface);
  return surface.translucent
    ? { ...style, className: `${style.className} surface-glass-controls` }
    : style;
}

/** A capsule takes the card's fill rule with its own settings. */
export function capsuleSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  return cardSurfaceStyle(surface);
}
