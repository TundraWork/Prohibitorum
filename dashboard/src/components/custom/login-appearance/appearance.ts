import type { LoginAppearance } from "@/api/raw-paths";

/**
 * The look of an instance that has never saved one, as `branding.DefaultAppearance`
 * defines it on the server: the page's own background, an opaque centred card,
 * translucent frosted capsules, and the theme left to the visitor.
 */
export const defaultLoginAppearance: LoginAppearance = {
  background: {
    source: "none",
    color: "#1f6f8b",
    gradient: "lagoon",
    bing: { market: "zh-CN", showCaption: true },
    unsplash: { query: "", order: "random", intervalSeconds: 10 },
    images: { order: "random", intervalSeconds: 10 },
  },
  card: { translucent: false, opacity: 80, blur: true },
  capsules: { translucent: true, opacity: 70, blur: true },
  cardPosition: "center",
  theme: "switchable",
};
