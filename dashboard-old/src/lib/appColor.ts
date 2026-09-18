/**
 * appColor — a deterministic, brand-harmonious tint for an app's letter-fallback
 * icon. The same seed always maps to the same hue, with no storage, so a given
 * app keeps a stable identity colour across loads, devices, and restarts (the
 * Gmail / Slack / Linear letter-avatar pattern).
 *
 * Only the HUE is dynamic. Lightness/chroma (and the light/dark split) live in
 * the `.app-tint` rule in main.css, so every tint is theme-correct and clears
 * WCAG AA by construction (verified ~7.6:1 light / ~8.3:1 dark across the ramp).
 *
 * Hues are a tight cool sweep (cyan → teal → blue → indigo → violet) that
 * deliberately AVOIDS the Sage (~150), Amber (~75), and Rose (~22) hues: those
 * are reserved for credential/session STATE, and an identity tint must never be
 * mistaken for a status signal.
 */
const APP_TINT_HUES = [188, 205, 222, 240, 258, 276, 294, 312] as const

/** djb2 string hash → unsigned 32-bit. Stable and cheap; not cryptographic. */
function hash(seed: string): number {
  let h = 5381
  for (let i = 0; i < seed.length; i++) {
    h = ((h << 5) + h + seed.charCodeAt(i)) >>> 0
  }
  return h
}

/** The OKLCH hue (degrees) for a seed, drawn from the harmonised tint ramp. */
export function appTintHue(seed: string): number {
  const s = (seed ?? '').trim() || '?'
  return APP_TINT_HUES[hash(s) % APP_TINT_HUES.length]!
}

/**
 * Convert an "#rrggbb" sRGB string to OKLCH. Returns null for malformed input.
 * Used to map an icon's server-extracted accent colour onto the tile backdrop:
 * we keep its hue and scale its chroma down to the calm backdrop range, so a
 * blue logo gets a faint blue backdrop and a grayscale logo a near-neutral one.
 */
export function srgbToOklch(hex: string): { l: number; c: number; h: number } | null {
  const m = /^#?([0-9a-f]{6})$/i.exec((hex ?? '').trim())
  if (!m) return null
  const n = parseInt(m[1]!, 16)
  const toLin = (u: number) => (u <= 0.04045 ? u / 12.92 : ((u + 0.055) / 1.055) ** 2.4)
  const r = toLin(((n >> 16) & 0xff) / 255)
  const g = toLin(((n >> 8) & 0xff) / 255)
  const b = toLin((n & 0xff) / 255)
  const l_ = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b)
  const m_ = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b)
  const s_ = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b)
  const L = 0.2104542553 * l_ + 0.793617785 * m_ - 0.0040720468 * s_
  const a = 1.9779984951 * l_ - 2.428592205 * m_ + 0.4505937099 * s_
  const bb = 0.0259040371 * l_ + 0.7827717662 * m_ - 0.808675766 * s_
  const c = Math.hypot(a, bb)
  let h = (Math.atan2(bb, a) * 180) / Math.PI
  if (h < 0) h += 360
  return { l: L, c, h }
}
