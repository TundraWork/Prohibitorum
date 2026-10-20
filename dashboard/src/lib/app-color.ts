/**
 * An application's identity colour on the launcher, as an OKLCH hue and a
 * chroma for the tile's cover.
 *
 * With an uploaded icon, the server extracts the icon's representative colour;
 * the cover keeps its hue and calms its chroma, so a blue logo sits on a faint
 * blue and a grey one on near-neutral. Without one, the hue comes from the
 * name, the same every time, from a cool sweep (cyan to violet) that stays clear
 * of the green, amber and red the console reserves for state: an identity tint
 * must never read as a status.
 */
const tintHues = [188, 205, 222, 240, 258, 276, 294, 312] as const;

/** The chroma of a cover tinted from an icon, at most: calm, not a swatch. */
const iconChromaMax = 0.03;
/** The chroma of a cover whose hue comes from the name. */
const nameChroma = 0.018;

export type AppTint = { hue: number; chroma: number };

export function appTint(name: string, accentColor?: string): AppTint {
  const accent = accentColor === undefined ? null : srgbToOklch(accentColor);
  if (accent !== null) {
    return { hue: accent.h, chroma: Math.min(iconChromaMax, accent.c) };
  }
  return { hue: hueForName(name), chroma: nameChroma };
}

/** A stable hue for a name: djb2 over its characters, picked from the sweep. */
export function hueForName(name: string): number {
  const seed = name.trim() || "?";
  let hash = 5381;
  for (let index = 0; index < seed.length; index++) {
    hash = ((hash << 5) + hash + seed.charCodeAt(index)) >>> 0;
  }
  return tintHues[hash % tintHues.length] ?? tintHues[0];
}

/** "#rrggbb" to OKLCH, or null for anything else. */
export function srgbToOklch(
  hex: string,
): { l: number; c: number; h: number } | null {
  const match = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (match?.[1] === undefined) return null;
  const value = Number.parseInt(match[1], 16);
  const linear = (channel: number) =>
    channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4;
  const r = linear(((value >> 16) & 0xff) / 255);
  const g = linear(((value >> 8) & 0xff) / 255);
  const b = linear((value & 0xff) / 255);
  const l_ = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m_ = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s_ = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  const l = 0.2104542553 * l_ + 0.793617785 * m_ - 0.0040720468 * s_;
  const a = 1.9779984951 * l_ - 2.428592205 * m_ + 0.4505937099 * s_;
  const bb = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.808675766 * s_;
  let h = (Math.atan2(bb, a) * 180) / Math.PI;
  if (h < 0) h += 360;
  return { l, c: Math.hypot(a, bb), h };
}
