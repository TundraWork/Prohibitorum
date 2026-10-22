/**
 * The colour schemes a reader can pick, in the order the pickers list them:
 * the ordinary ones, then the youthful ones. A scheme is a set of semantic
 * tokens in src/styles/schemes.css, applied as `data-scheme` on `<html>`;
 * light and dark are a separate choice, so every scheme has both. The swatch
 * colours (the page surface and the accent) draw the pickers' previews.
 *
 * The server keeps the same slugs (pkg/branding), and a test checks the two
 * lists agree.
 */
export const colorSchemes = [
  {
    slug: "graphite",
    kind: "ordinary",
    swatch: {
      light: { surface: "#ffffff", accent: "#262626" },
      dark: { surface: "#121212", accent: "#f5f5f5" },
    },
  },
  {
    slug: "nord",
    kind: "ordinary",
    swatch: {
      light: { surface: "#eceff4", accent: "#406593" },
      dark: { surface: "#292e39", accent: "#88c0d0" },
    },
  },
  {
    slug: "sand",
    kind: "ordinary",
    swatch: {
      light: { surface: "#fdfdfc", accent: "#553c2b" },
      dark: { surface: "#111110", accent: "#dfc1a6" },
    },
  },
  {
    slug: "matcha",
    kind: "ordinary",
    swatch: {
      light: { surface: "#fdf6e3", accent: "#5c6f21" },
      dark: { surface: "#272e33", accent: "#a7c080" },
    },
  },
  {
    slug: "sakura",
    kind: "youthful",
    swatch: {
      light: { surface: "#fff8fb", accent: "#c33278" },
      dark: { surface: "#1a1017", accent: "#f7a0c0" },
    },
  },
  {
    slug: "lavender",
    kind: "youthful",
    swatch: {
      light: { surface: "#eff1f5", accent: "#8839ef" },
      dark: { surface: "#1e1e2e", accent: "#cba6f7" },
    },
  },
  {
    slug: "lagoon",
    kind: "youthful",
    swatch: {
      light: { surface: "#f3fcfb", accent: "#117393" },
      dark: { surface: "#06161c", accent: "#5ddae0" },
    },
  },
  {
    slug: "citrus",
    kind: "youthful",
    swatch: {
      light: { surface: "#fdfcf3", accent: "#eae14a" },
      dark: { surface: "#15140e", accent: "#e7ea6a" },
    },
  },
  {
    slug: "catppuccin",
    kind: "youthful",
    swatch: {
      light: { surface: "#eff1f5", accent: "#8839ef" },
      dark: { surface: "#24273a", accent: "#c6a0f6" },
    },
  },
  {
    slug: "catppuccin-gray",
    kind: "youthful",
    swatch: {
      light: { surface: "#f1f1f1", accent: "#8839ef" },
      dark: { surface: "#282828", accent: "#c6a0f6" },
    },
  },
] as const;

export type ColorSchemeSlug = (typeof colorSchemes)[number]["slug"];

/** What every page shows until an administrator or a reader chooses. */
export const defaultColorScheme: ColorSchemeSlug = "graphite";

const slugs: ReadonlySet<string> = new Set(colorSchemes.map((s) => s.slug));

/** Whether a stored or received value names a scheme this build knows. */
export function isColorSchemeSlug(value: unknown): value is ColorSchemeSlug {
  return typeof value === "string" && slugs.has(value);
}
