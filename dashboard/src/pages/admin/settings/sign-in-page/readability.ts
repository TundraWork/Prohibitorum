import type { LoginAppearance } from "@/api/raw-paths";
import { gradientBackground } from "@/components/custom/login-appearance/gradients";

/** WCAG's AA line for body text. */
export const readableContrast = 4.5;

export interface Rgb {
  r: number;
  g: number;
  b: number;
}

export interface Rgba extends Rgb {
  a: number;
}

export interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** A sampled region, row by row. */
export interface Grid {
  cols: number;
  rows: number;
  cells: Rgb[];
}

/** A surface drawn over the backdrop; `blur` is the standard deviation, in CSS pixels, of the glass behind it. */
export interface Layer {
  fill: Rgba;
  blur: number;
}

/** The frosted frame the card sits in (when there is one) and the card itself. */
export interface Layers {
  frame?: Layer;
  card: Layer;
}

function channel(value: number): number {
  const s = value / 255;
  return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

export function relativeLuminance({ r, g, b }: Rgb): number {
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/** WCAG's contrast ratio, 1 to 21, whichever colour is lighter. */
export function contrastRatio(a: Rgb, b: Rgb): number {
  const la = relativeLuminance(a);
  const lb = relativeLuminance(b);
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

/** `top` drawn over an opaque `below`. */
export function over(top: Rgba, below: Rgb): Rgb {
  return {
    r: top.r * top.a + below.r * (1 - top.a),
    g: top.g * top.a + below.g * (1 - top.a),
    b: top.b * top.a + below.b * (1 - top.a),
  };
}

/** Moves along one axis, averaging the cells within `radius` (fewer at the edges). */
function blurAxis(grid: Grid, radius: number, vertical: boolean): Grid {
  if (radius <= 0) return grid;
  const { cols, rows } = grid;
  const length = vertical ? rows : cols;
  const lines = vertical ? cols : rows;
  const at = (line: number, i: number) =>
    grid.cells[vertical ? i * cols + line : line * cols + i] as Rgb;
  const cells = new Array<Rgb>(cols * rows);
  for (let line = 0; line < lines; line++) {
    for (let i = 0; i < length; i++) {
      const from = Math.max(0, i - radius);
      const to = Math.min(length - 1, i + radius);
      let r = 0;
      let g = 0;
      let b = 0;
      for (let k = from; k <= to; k++) {
        const cell = at(line, k);
        r += cell.r;
        g += cell.g;
        b += cell.b;
      }
      const n = to - from + 1;
      cells[vertical ? i * cols + line : line * cols + i] = {
        r: r / n,
        g: g / n,
        b: b / n,
      };
    }
  }
  return { cols, rows, cells };
}

/** A box blur, with radii in cells. */
export function blurGrid(grid: Grid, radiusX: number, radiusY: number): Grid {
  return blurAxis(blurAxis(grid, radiusX, false), radiusY, true);
}

/**
 * The radius, in cells, of a box blur that spreads light as far as a Gaussian
 * of standard deviation `sigmaPx` does (a box of radius r has deviation r/√3).
 */
function blurRadius(sigmaPx: number, cellPx: number): number {
  return Math.round((sigmaPx * Math.sqrt(3)) / cellPx);
}

/**
 * The part of a picture, in its own pixels, that shows under `area` of a
 * window the picture is drawn into with `object-fit: cover`.
 */
export function coverSource(
  view: { width: number; height: number },
  picture: { width: number; height: number },
  area: Box,
): Box {
  const scale = Math.max(
    view.width / picture.width,
    view.height / picture.height,
  );
  const offsetX = (view.width - picture.width * scale) / 2;
  const offsetY = (view.height - picture.height * scale) / 2;
  return {
    x: (area.x - offsetX) / scale,
    y: (area.y - offsetY) / scale,
    width: area.width / scale,
    height: area.height / scale,
  };
}

/** Splits CSS at the commas outside any parentheses. */
function splitTopLevel(css: string): string[] {
  const parts: string[] = [];
  let depth = 0;
  let start = 0;
  for (let i = 0; i < css.length; i++) {
    const c = css[i];
    if (c === "(") depth++;
    else if (c === ")") depth--;
    else if (c === "," && depth === 0) {
      parts.push(css.slice(start, i).trim());
      start = i + 1;
    }
  }
  parts.push(css.slice(start).trim());
  return parts;
}

/**
 * The colours a gradient preset passes through: its base ramp, and each
 * overlay's stops laid over every colour of that ramp at their own opacity. A
 * gradient is not sampled by position, so any of them may be behind the card.
 * The fine grain over it is left out. `undefined` when a colour cannot be read.
 */
export function gradientSamples(
  css: string,
  resolve: (css: string) => Rgba | undefined,
): Rgb[] | undefined {
  const layers = splitTopLevel(css).filter(
    (layer) => !layer.startsWith("url("),
  );
  const stops = (layer: string) =>
    (layer.match(/oklch\([^)]*\)/g) ?? []).map(resolve);
  const base = stops(layers[layers.length - 1] ?? "");
  const overlays = layers.slice(0, -1).flatMap(stops);
  if ([...base, ...overlays].some((color) => color === undefined)) {
    return undefined;
  }
  const ramp = base as Rgba[];
  return [
    ...ramp.map(({ r, g, b }) => ({ r, g, b })),
    ...(overlays as Rgba[])
      .filter((stop) => stop.a > 0)
      .flatMap((stop) => ramp.map((below) => over(stop, below))),
  ];
}

/** The text colour's lowest contrast across colours that may be behind the card. */
export function worstContrastOnColors(
  colors: Rgb[],
  layers: Layers,
  foreground: Rgb,
): number {
  let worst = Number.POSITIVE_INFINITY;
  for (const behind of colors) {
    const framed = layers.frame ? over(layers.frame.fill, behind) : behind;
    worst = Math.min(
      worst,
      contrastRatio(foreground, over(layers.card.fill, framed)),
    );
  }
  return worst;
}

/**
 * The text colour's lowest contrast across the cells under the card.
 * `region` is wider than the card, so each glass layer's blur gathers light
 * from beyond its edge; `inner` is the cells the card covers.
 */
export function worstContrastOnGrid(
  region: Grid,
  cell: { width: number; height: number },
  inner: { col: number; row: number; cols: number; rows: number },
  layers: Layers,
  foreground: Rgb,
): number {
  const blur = (grid: Grid, sigma: number) =>
    sigma > 0
      ? blurGrid(
          grid,
          blurRadius(sigma, cell.width),
          blurRadius(sigma, cell.height),
        )
      : grid;
  let grid = region;
  if (layers.frame) {
    const { fill, blur: sigma } = layers.frame;
    grid = blur(grid, sigma);
    grid = { ...grid, cells: grid.cells.map((c) => over(fill, c)) };
  }
  grid = blur(grid, layers.card.blur);
  let worst = Number.POSITIVE_INFINITY;
  for (let row = inner.row; row < inner.row + inner.rows; row++) {
    for (let col = inner.col; col < inner.col + inner.cols; col++) {
      const behind = grid.cells[row * grid.cols + col];
      if (behind === undefined) continue;
      worst = Math.min(
        worst,
        contrastRatio(foreground, over(layers.card.fill, behind)),
      );
    }
  }
  return worst;
}

/** A loaded picture that can hand back a downsampled part of itself. */
export interface Picture {
  width: number;
  height: number;
  /** `undefined` when the browser will not release the pixels. */
  sample: (source: Box, cols: number, rows: number) => Grid | undefined;
}

/** What measuring needs from the browser, so tests can stand in for it. */
export interface Environment {
  /** Any CSS colour as sRGB; `undefined` when it cannot be read. */
  resolveColor: (css: string) => Rgba | undefined;
  /** `undefined` when the picture cannot be loaded for reading. */
  loadPicture: (url: string) => Promise<Picture | undefined>;
}

let probe: CanvasRenderingContext2D | null | undefined;
const colorCache = new Map<string, Rgba | undefined>();
const maxColors = 200;
const maxPictures = 8;
const maxFailures = 50;

/** Sets a cache entry as the newest, dropping the oldest past `max`. */
export function remember<K, V>(
  cache: Map<K, V>,
  key: K,
  value: V,
  max: number,
) {
  cache.delete(key);
  cache.set(key, value);
  if (cache.size > max) {
    const oldest = cache.keys().next();
    if (!oldest.done) cache.delete(oldest.value);
  }
}

function resolveCanvasColor(css: string): Rgba | undefined {
  if (colorCache.has(css)) return colorCache.get(css);
  if (probe === undefined) {
    probe = document
      .createElement("canvas")
      .getContext("2d", { willReadFrequently: true });
    if (probe) {
      probe.canvas.width = 1;
      probe.canvas.height = 1;
    }
  }
  let color: Rgba | undefined;
  // Text that is not a colour would otherwise read as the transparent it
  // started from, and be measured as if it were one.
  if (probe && CSS.supports("color", css)) {
    probe.clearRect(0, 0, 1, 1);
    probe.fillStyle = "rgba(0, 0, 0, 0)";
    probe.fillStyle = css;
    probe.fillRect(0, 0, 1, 1);
    const [r = 0, g = 0, b = 0, a = 0] = probe.getImageData(0, 0, 1, 1).data;
    color = { r, g, b, a: a / 255 };
  }
  remember(colorCache, css, color, maxColors);
  return color;
}

const pictures = new Map<string, Promise<Picture | undefined>>();
/** Addresses that would not load for reading, so a host without CORS is asked once. */
const failedPictures = new Map<string, true>();

/**
 * Loads the picture again for reading, with CORS asked for: the copy the
 * preview shows may have come without it, and a canvas drawn from that one
 * refuses to release its pixels. A picture that sends no CORS headers fails
 * here, and so cannot be measured.
 */
function loadCanvasPicture(url: string): Promise<Picture | undefined> {
  if (failedPictures.has(url)) return Promise.resolve(undefined);
  const known = pictures.get(url);
  if (known) return known;
  const loading = new Promise<Picture | undefined>((resolve) => {
    const image = new Image();
    image.crossOrigin = "anonymous";
    image.onerror = () => resolve(undefined);
    image.onload = () =>
      resolve({
        width: image.naturalWidth,
        height: image.naturalHeight,
        sample: (source, cols, rows) => {
          const canvas = document.createElement("canvas");
          canvas.width = cols;
          canvas.height = rows;
          const context = canvas.getContext("2d", {
            willReadFrequently: true,
          });
          if (!context) return undefined;
          context.imageSmoothingQuality = "high";
          context.drawImage(
            image,
            source.x,
            source.y,
            source.width,
            source.height,
            0,
            0,
            cols,
            rows,
          );
          try {
            const { data } = context.getImageData(0, 0, cols, rows);
            const cells: Rgb[] = [];
            for (let i = 0; i < data.length; i += 4) {
              cells.push({
                r: data[i] ?? 0,
                g: data[i + 1] ?? 0,
                b: data[i + 2] ?? 0,
              });
            }
            return { cols, rows, cells };
          } catch {
            return undefined;
          }
        },
      });
    image.src = url;
  });
  remember(pictures, url, loading, maxPictures);
  void loading.then((picture) => {
    if (picture !== undefined) return;
    pictures.delete(url);
    remember(failedPictures, url, true, maxFailures);
  });
  return loading;
}

export const canvasEnvironment: Environment = {
  resolveColor: resolveCanvasColor,
  loadPicture: loadCanvasPicture,
};

export type ReadabilityResult =
  | { kind: "measured"; ratio: number; readable: boolean }
  | { kind: "unmeasurable"; cause: UnmeasurableCause };

/** Whether it is the background that cannot be read, or the page being measured. */
export type UnmeasurableCause = "background" | "page";

const unmeasurable: ReadabilityResult = {
  kind: "unmeasurable",
  cause: "background",
};
const pageUnmeasurable: ReadabilityResult = {
  kind: "unmeasurable",
  cause: "page",
};

function measured(ratio: number): ReadabilityResult {
  return { kind: "measured", ratio, readable: ratio >= readableContrast };
}

function blurOf(style: CSSStyleDeclaration): number {
  const filter =
    style.getPropertyValue("backdrop-filter") ||
    style.getPropertyValue("-webkit-backdrop-filter");
  const match = /blur\(\s*([\d.]+)px\s*\)/.exec(filter);
  return match?.[1] ? Number(match[1]) : 0;
}

/** Cells are about this many CSS pixels across. */
const cellPx = 12;
/** The most cells along a side. */
const maxCells = 80;

/**
 * How readable the card's text is where the preview draws it: the contrast of
 * the card's text colour against the worst place behind the card, with the
 * glass layers between them (the frame's and the card's own fill and blur)
 * worked out from the preview's own computed styles.
 *
 * An opaque card needs no look at the background. Colours come from the
 * settings, a gradient from its preset's colours, and a picture from the part
 * under the card's rectangle. Anything that cannot be read is `unmeasurable`,
 * never a guess. Not modelled: the frame's and card's saturation boost, and
 * the picture's fade while it appears.
 */
export async function measureReadability(
  doc: Document,
  appearance: LoginAppearance,
  environment: Environment,
): Promise<ReadabilityResult> {
  const view = doc.defaultView;
  const card = doc.querySelector<HTMLElement>('[data-slot="stage-card"]');
  if (!view || !card) return pageUnmeasurable;
  const { resolveColor, loadPicture } = environment;

  const cardStyle = view.getComputedStyle(card);
  const foreground = resolveColor(cardStyle.color);
  const cardFill = resolveColor(cardStyle.backgroundColor);
  if (!foreground || !cardFill) return pageUnmeasurable;
  if (cardFill.a >= 0.995) return measured(contrastRatio(foreground, cardFill));

  const frame = doc.querySelector<HTMLElement>('[data-slot="stage-frame"]');
  const frameStyle = frame ? view.getComputedStyle(frame) : undefined;
  const frameFill = frameStyle && resolveColor(frameStyle.backgroundColor);
  // A frame whose colour cannot be read may be hiding or tinting the backdrop.
  if (frameStyle && !frameFill) return pageUnmeasurable;
  const layers: Layers = {
    card: { fill: cardFill, blur: blurOf(cardStyle) },
    ...(frameStyle && frameFill
      ? { frame: { fill: frameFill, blur: blurOf(frameStyle) } }
      : {}),
  };

  const pageBackground = (): Rgb[] | undefined => {
    for (const element of [doc.body, doc.documentElement]) {
      const color = resolveColor(
        view.getComputedStyle(element).backgroundColor,
      );
      if (color && color.a > 0)
        return [over(color, { r: 255, g: 255, b: 255 })];
    }
    return undefined;
  };
  const worstOn = (colors: Rgb[] | undefined): ReadabilityResult =>
    colors === undefined || colors.length === 0
      ? unmeasurable
      : measured(worstContrastOnColors(colors, layers, foreground));

  const background = appearance.background;
  switch (background.source) {
    case "none":
      return worstOn(pageBackground());
    case "color": {
      const color = resolveColor(background.color);
      return worstOn(color && [over(color, { r: 255, g: 255, b: 255 })]);
    }
    case "gradient":
      return worstOn(
        gradientSamples(gradientBackground[background.gradient], resolveColor),
      );
    case "bing":
    case "unsplash":
    case "images": {
      const image = doc.querySelector<HTMLImageElement>(
        '[data-login-backdrop="pictures"] img[data-current]',
      );
      // Uploaded images with none uploaded leave the page's own background.
      if (!image) {
        return background.source === "images"
          ? worstOn(pageBackground())
          : unmeasurable;
      }
      const picture = await loadPicture(image.currentSrc || image.src);
      if (!picture || picture.width === 0 || picture.height === 0) {
        return unmeasurable;
      }
      return measurePicture(doc, card, picture, layers, foreground);
    }
    default:
      return unmeasurable;
  }
}

function measurePicture(
  doc: Document,
  card: HTMLElement,
  picture: Picture,
  layers: Layers,
  foreground: Rgb,
): ReadabilityResult {
  const view = {
    width: doc.documentElement.clientWidth,
    height: doc.documentElement.clientHeight,
  };
  const rect = card.getBoundingClientRect();
  if (view.width === 0 || view.height === 0 || rect.width === 0) {
    return pageUnmeasurable;
  }
  // Wide enough that each blur gathers the light it would from beyond the card.
  const reach = Math.ceil(
    Math.sqrt(3) * ((layers.frame?.blur ?? 0) + layers.card.blur),
  );
  const left = Math.max(0, rect.left - reach);
  const top = Math.max(0, rect.top - reach);
  const area: Box = {
    x: left,
    y: top,
    width: Math.min(view.width, rect.right + reach) - left,
    height: Math.min(view.height, rect.bottom + reach) - top,
  };
  if (area.width <= 0 || area.height <= 0) return pageUnmeasurable;
  const cols = Math.min(maxCells, Math.max(1, Math.ceil(area.width / cellPx)));
  const rows = Math.min(maxCells, Math.max(1, Math.ceil(area.height / cellPx)));
  const region = picture.sample(coverSource(view, picture, area), cols, rows);
  if (!region) return unmeasurable;

  const cell = { width: area.width / cols, height: area.height / rows };
  const first = (edge: number, origin: number, size: number) =>
    Math.max(0, Math.floor((edge - origin) / size));
  const col = first(rect.left, area.x, cell.width);
  const row = first(rect.top, area.y, cell.height);
  const inner = {
    col,
    row,
    cols: Math.max(
      1,
      Math.min(cols, Math.ceil((rect.right - area.x) / cell.width)) - col,
    ),
    rows: Math.max(
      1,
      Math.min(rows, Math.ceil((rect.bottom - area.y) / cell.height)) - row,
    ),
  };
  return measured(worstContrastOnGrid(region, cell, inner, layers, foreground));
}

/**
 * Calls `onChange`, once after `delay` ms of quiet, when anything that decides
 * the measurement may have changed in the preview: its theme, the page's
 * structure or styles (a picture arriving, the card's surface, its position),
 * a picture finishing loading, or the window's size. Returns a function that
 * stops watching.
 */
export function watchPreview(
  doc: Document,
  onChange: () => void,
  delay = 200,
): () => void {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const changed = () => {
    clearTimeout(timer);
    timer = setTimeout(onChange, delay);
  };
  const observer = new MutationObserver(changed);
  observer.observe(doc.documentElement, {
    attributes: true,
    attributeFilter: ["class", "data-theme"],
  });
  observer.observe(doc.body, {
    attributes: true,
    childList: true,
    subtree: true,
  });
  // Load events do not bubble, but they can be caught on the way down.
  doc.addEventListener("load", changed, true);
  const view = doc.defaultView;
  view?.addEventListener("resize", changed);
  return () => {
    clearTimeout(timer);
    observer.disconnect();
    doc.removeEventListener("load", changed, true);
    view?.removeEventListener("resize", changed);
  };
}
