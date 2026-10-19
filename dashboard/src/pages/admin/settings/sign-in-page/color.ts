export interface Hsb {
  /** 0–360. */
  h: number;
  /** 0–100. */
  s: number;
  /** 0–100. */
  b: number;
}

const fallback = "#1f6f8b";
const hexColor = /^#[0-9a-f]{6}$/i;

/** Hue, saturation and brightness of `#rrggbb`; text that is not a colour reads as the theme's teal. */
export function hexToHsb(hex: string): Hsb {
  const source = hexColor.test(hex) ? hex : fallback;
  const [r = 0, g = 0, b = 0] = [1, 3, 5].map(
    (at) => Number.parseInt(source.slice(at, at + 2), 16) / 255,
  );
  const max = Math.max(r, g, b);
  const delta = max - Math.min(r, g, b);
  let hue = 0;
  if (delta !== 0) {
    if (max === r) hue = ((g - b) / delta) % 6;
    else if (max === g) hue = (b - r) / delta + 2;
    else hue = (r - g) / delta + 4;
  }
  return {
    h: Math.round((hue * 60 + 360) % 360),
    s: Math.round(max === 0 ? 0 : (delta / max) * 100),
    b: Math.round(max * 100),
  };
}

/** Lowercase `#rrggbb` for a hue, saturation and brightness. */
export function hsbToHex({ h, s, b }: Hsb): string {
  const saturation = s / 100;
  const value = b / 100;
  const channel = (offset: number) => {
    const k = (offset + h / 60) % 6;
    return value - value * saturation * Math.max(0, Math.min(k, 4 - k, 1));
  };
  return `#${[5, 3, 1]
    .map((offset) =>
      Math.round(channel(offset) * 255)
        .toString(16)
        .padStart(2, "0"),
    )
    .join("")}`;
}

/** The colour with only its hue changed. */
export function withHue(hex: string, hue: number): string {
  return hsbToHex({ ...hexToHsb(hex), h: hue });
}
