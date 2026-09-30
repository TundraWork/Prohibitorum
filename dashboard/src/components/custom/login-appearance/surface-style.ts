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
 * On a translucent card, the controls drawn on it take a fill of half the
 * card's opacity, so they are clearer than the card and the picture reads
 * through the fields as well. A fill is a tint of `--foreground` laid over
 * `--background`, which keeps it apart from the card and its label in contrast
 * with it in either theme; `--surface-alpha`, the opacity the settings chose, is
 * halved for it. The values are built from `--foreground` and `--background`
 * because a property cannot be redefined from its own value. The hover fills
 * (`--field-hover`, `--default-hover`, `--surface-hover`) are resolved at
 * `:root` from the opaque colours, so they are set here too. Popovers keep
 * `--overlay`, which stays opaque.
 */
const translucentControls = [
  "[--field-background:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_6%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--field-hover:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_10%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--default:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_12%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--default-hover:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_18%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--surface-secondary:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_6%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--surface-tertiary:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_12%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--surface-hover:color-mix(in_oklab,color-mix(in_oklab,var(--foreground)_10%,var(--background))_calc(var(--surface-alpha)*0.5),transparent)]",
  "[--separator:color-mix(in_oklab,var(--foreground)_calc(var(--surface-alpha)*0.2),transparent)]",
  "[--border:color-mix(in_oklab,var(--foreground)_calc(var(--surface-alpha)*0.25),transparent)]",
].join(" ");

/**
 * A translucent surface that carries controls — the card, and the capsules' buttons — also gives the controls drawn on it
 * their see-through fills, so the public page and the settings preview agree.
 */
export function cardSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  const style = surfaceStyle(surface);
  return surface.translucent
    ? { ...style, className: `${style.className} ${translucentControls}` }
    : style;
}

/**
 * A capsule has no fill of its own, so an opaque one takes the surface colour
 * rather than nothing. A translucent one gives the buttons on it the same
 * see-through fills as the card.
 */
export function capsuleSurfaceStyle(surface: LoginSurface): SurfaceStyle {
  return surface.translucent
    ? cardSurfaceStyle(surface)
    : { className: "bg-surface" };
}
