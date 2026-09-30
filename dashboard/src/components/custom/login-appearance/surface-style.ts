import { cva } from "class-variance-authority";
import type { CSSProperties } from "react";
import type { LoginSurface } from "@/api/raw-paths";

/**
 * A translucent surface keeps HeroUI's own fill and shadow classes and only
 * lowers the fill's opacity through one custom property, set inline; frosted
 * glass blurs and lifts what shows through. An opaque surface gets nothing, so
 * the library draws it exactly as it would anywhere else.
 */
export const translucentSurface = cva("", {
  variants: {
    translucent: {
      true: "bg-[color-mix(in_oklab,var(--surface)_var(--surface-alpha),transparent)]",
      false: "",
    },
    blur: { true: "", false: "" },
  },
  compoundVariants: [
    {
      translucent: true,
      blur: true,
      className: "backdrop-blur-xl backdrop-saturate-150",
    },
  ],
});

export interface SurfaceStyle {
  className: string;
  style?: CSSProperties;
}

export function surfaceStyle(surface: LoginSurface): SurfaceStyle {
  if (!surface.translucent) return { className: "" };
  return {
    className: translucentSurface({ translucent: true, blur: surface.blur }),
    style: { "--surface-alpha": `${surface.opacity}%` } as CSSProperties,
  };
}

/**
 * A capsule has no fill of its own, so an opaque one takes the surface colour
 * rather than nothing.
 */
export function capsuleSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  return surface.translucent
    ? surfaceStyle(surface)
    : { className: "bg-surface" };
}
