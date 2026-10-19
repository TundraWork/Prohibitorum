import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginAppearance } from "@/api/raw-paths";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import {
  blurGrid,
  canvasEnvironment,
  contrastRatio,
  coverSource,
  type Environment,
  type Grid,
  gradientSamples,
  measureReadability,
  over,
  type Rgba,
  readableContrast,
  remember,
  settleOver,
  watchPreview,
  worstContrastOnColors,
  worstContrastOnGrid,
} from "@/pages/admin/settings/sign-in-page/readability";

const black = { r: 0, g: 0, b: 0 };
const white = { r: 255, g: 255, b: 255 };

describe("contrast math", () => {
  it.each([
    [black, white, 21],
    [white, white, 1],
    [{ r: 118, g: 118, b: 118 }, white, 4.54],
    [{ r: 0, g: 0, b: 255 }, white, 8.59],
  ])("%j on %j is %d : 1", (a, b, expected) => {
    expect(contrastRatio(a, b)).toBeCloseTo(expected, 1);
    expect(contrastRatio(b, a)).toBeCloseTo(expected, 1);
  });

  it("puts the readable line at 4.5", () => {
    expect(readableContrast).toBe(4.5);
  });
});

describe("laying one colour over another", () => {
  it("keeps the colour below under a clear layer and the layer when solid", () => {
    expect(over({ ...white, a: 0 }, black)).toEqual(black);
    expect(over({ ...white, a: 1 }, black)).toEqual(white);
  });

  it("mixes by the layer's opacity", () => {
    const mixed = over({ ...white, a: 0.5 }, black);
    expect(mixed.r).toBeCloseTo(127.5);
    expect(mixed.g).toBeCloseTo(127.5);
  });
});

describe("settling a picture over the page behind it", () => {
  it("shows the page through a see-through pixel, not black", () => {
    const settled = settleOver(
      {
        cols: 3,
        rows: 1,
        cells: [
          { r: 10, g: 20, b: 30 },
          { r: 0, g: 0, b: 0, a: 0 },
          { r: 255, g: 255, b: 255, a: 0.5 },
        ],
      },
      { r: 200, g: 200, b: 200 },
    );
    expect(settled.cells[0]).toEqual({ r: 10, g: 20, b: 30 });
    expect(settled.cells[1]).toEqual({ r: 200, g: 200, b: 200 });
    expect(settled.cells[2]?.r).toBeCloseTo(227.5);
  });
});

function grid(
  cols: number,
  rows: number,
  at: (c: number, r: number) => number,
) {
  const cells = [];
  for (let r = 0; r < rows; r++)
    for (let c = 0; c < cols; c++) {
      const v = at(c, r);
      cells.push({ r: v, g: v, b: v });
    }
  return { cols, rows, cells } satisfies Grid;
}

describe("blurring a sampled region", () => {
  it("leaves a flat region alone and a zero radius unchanged", () => {
    const flat = grid(5, 5, () => 40);
    expect(blurGrid(flat, 2, 2).cells.every((c) => c.r === 40)).toBe(true);
    const spike = grid(5, 1, (c) => (c === 2 ? 250 : 0));
    expect(blurGrid(spike, 0, 0).cells).toEqual(spike.cells);
  });

  it("spreads a bright cell over its neighbours without adding light", () => {
    const spike = grid(5, 1, (c) => (c === 2 ? 250 : 0));
    const blurred = blurGrid(spike, 1, 0);
    expect(blurred.cells.map((c) => c.r)).toEqual([
      0,
      250 / 3,
      250 / 3,
      250 / 3,
      0,
    ]);
  });
});

describe("where the picture sits behind the card", () => {
  it("maps a window area onto a picture drawn to cover it", () => {
    // 1600x900 covers 1280x800 at 0.8889, so 80px of the picture is cropped each side.
    const box = coverSource(
      { width: 1280, height: 800 },
      { width: 1600, height: 900 },
      { x: 0, y: 0, width: 1280, height: 800 },
    );
    expect(box.x).toBeCloseTo(80);
    expect(box.y).toBeCloseTo(0);
    expect(box.width).toBeCloseTo(1440);
    expect(box.height).toBeCloseTo(900);
  });

  it("maps an inner area by the same scale and offset", () => {
    const box = coverSource(
      { width: 1000, height: 1000 },
      { width: 500, height: 500 },
      { x: 100, y: 200, width: 400, height: 300 },
    );
    expect(box).toEqual({ x: 50, y: 100, width: 200, height: 150 });
  });
});

/** Colours as `rgb(r,g,b)` / `rgba(...)` text, for the tests' stand-in resolver. */
function parseColor(css: string): Rgba | undefined {
  const named: Record<string, Rgba> = {
    white: { ...white, a: 1 },
    black: { ...black, a: 1 },
  };
  if (css in named) return named[css];
  const m = /^rgba?\(([^)]*)\)$/.exec(css);
  if (!m?.[1]) return undefined;
  const [r = 0, g = 0, b = 0, a = 1] = m[1].split(",").map(Number);
  return { r, g, b, a };
}

describe("a gradient preset's colours", () => {
  // oklch() text stands in for itself: the resolver below reads "oklch(L)" as grey L.
  const resolve = (css: string): Rgba | undefined => {
    const m = /oklch\((\d+)%(?: [\d.]+ [\d.]+)?(?: \/ ([\d.]+))?\)/.exec(css);
    if (!m) return undefined;
    const v = Math.round((Number(m[1]) / 100) * 255);
    return { r: v, g: v, b: v, a: m[2] === undefined ? 1 : Number(m[2]) };
  };

  it("takes the base ramp and every overlay stop laid over each base colour", () => {
    const css = [
      'url("data:image/svg+xml,%3Csvg%3E")',
      "radial-gradient(60% 50% at 18% 28%, oklch(100% 0.1 160 / 0.5), transparent 70%)",
      "linear-gradient(180deg, oklch(0% 0 0), oklch(40% 0 0))",
    ].join(", ");
    const samples = gradientSamples(css, resolve);
    expect(samples?.map((s) => Math.round(s.r))).toEqual([0, 102, 128, 179]);
  });

  it("says nothing when a colour cannot be read", () => {
    expect(
      gradientSamples(
        "linear-gradient(0deg, oklch(10%), oklch(20%))",
        () => undefined,
      ),
    ).toBeUndefined();
  });

  it("reads every preset the settings offer", async () => {
    const { gradientBackground } = await import(
      "@/components/custom/login-appearance/gradients"
    );
    for (const css of Object.values(gradientBackground)) {
      expect(gradientSamples(css, resolve)?.length).toBeGreaterThanOrEqual(2);
    }
  });
});

describe("the card's surface over what is behind it", () => {
  const frameless = { card: { fill: { ...white, a: 0.5 }, blur: 0 } };

  it("takes the worst of the colours behind a translucent card", () => {
    // Half-white over black is grey 127.5; over white it is white.
    const worst = worstContrastOnColors([black, white], frameless, black);
    expect(worst).toBeCloseTo(
      contrastRatio(black, { r: 127.5, g: 127.5, b: 127.5 }),
    );
  });

  it("lays the frame under the card first", () => {
    const layers = {
      frame: { fill: { ...black, a: 1 }, blur: 0 },
      card: { fill: { ...white, a: 0.5 }, blur: 0 },
    };
    // The frame hides the backdrop: the card is half-white over black.
    expect(worstContrastOnColors([white], layers, black)).toBeCloseTo(
      contrastRatio(black, { r: 127.5, g: 127.5, b: 127.5 }),
    );
  });

  it("uses only the cells under the card, after the blur has spread the light", () => {
    // A bright stripe at the left edge, outside the card's cells.
    const region = grid(5, 1, (c) => (c === 0 ? 255 : 0));
    const inner = { col: 2, row: 0, cols: 3, rows: 1 };
    const cell = { width: 10, height: 10 };
    const sharp = worstContrastOnGrid(
      region,
      cell,
      inner,
      { card: { fill: { ...black, a: 0 }, blur: 0 } },
      white,
    );
    expect(sharp).toBeCloseTo(21);
    // 5px of blur is a one-cell radius at 10px cells: light reaches cell 1 only.
    const near = worstContrastOnGrid(
      region,
      cell,
      inner,
      { card: { fill: { ...black, a: 0 }, blur: 5 } },
      white,
    );
    expect(near).toBeCloseTo(21);
    // 60px of blur reaches the card's own cells.
    const far = worstContrastOnGrid(
      region,
      cell,
      inner,
      { card: { fill: { ...black, a: 0 }, blur: 60 } },
      white,
    );
    expect(far).toBeLessThan(21);
  });
});

/* The preview page, as the measurement reads it. */

interface Page {
  doc: Document;
  card: HTMLElement;
  frame: HTMLElement;
  rect: (left: number, top: number, width: number, height: number) => void;
}

const frames: HTMLIFrameElement[] = [];
afterEach(() => {
  for (const frame of frames.splice(0)) frame.remove();
});

function previewPage(
  card: { color: string; background: string },
  frame?: { background: string },
): Page {
  const holder = document.createElement("iframe");
  document.body.append(holder);
  frames.push(holder);
  const doc = holder.contentDocument as Document;
  doc.body.style.backgroundColor = "rgb(255, 255, 255)";
  const frameEl = doc.createElement("section");
  frameEl.dataset.slot = "stage-frame";
  frameEl.style.backgroundColor = frame?.background ?? "rgba(0, 0, 0, 0)";
  const cardEl = doc.createElement("div");
  cardEl.dataset.slot = "stage-card";
  cardEl.style.color = card.color;
  cardEl.style.backgroundColor = card.background;
  frameEl.append(cardEl);
  doc.body.append(frameEl);
  const rect = (left: number, top: number, width: number, height: number) =>
    vi.spyOn(cardEl, "getBoundingClientRect").mockReturnValue({
      left,
      top,
      width,
      height,
      right: left + width,
      bottom: top + height,
      x: left,
      y: top,
      toJSON: () => ({}),
    });
  rect(100, 100, 400, 300);
  Object.defineProperty(doc.documentElement, "clientWidth", {
    value: 1280,
    configurable: true,
  });
  Object.defineProperty(doc.documentElement, "clientHeight", {
    value: 800,
    configurable: true,
  });
  return { doc, card: cardEl, frame: frameEl, rect };
}

function withBackground(edit: (b: LoginAppearance["background"]) => void) {
  const appearance = structuredClone(defaultLoginAppearance);
  edit(appearance.background);
  return appearance;
}

const noEnvironment: Environment = {
  resolveColor: (css) => parseColor(css.replaceAll(" ", "")),
  loadPicture: vi.fn(async () => undefined),
};

describe("measuring the sign-in card", () => {
  it("needs no picture while the card is opaque", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgb(255,255,255)",
    });
    const loadPicture = vi.fn(async () => undefined);
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "unsplash";
      }),
      { ...noEnvironment, loadPicture },
    );
    expect(result).toEqual({ kind: "measured", ratio: 21, readable: true });
    expect(loadPicture).not.toHaveBeenCalled();
  });

  it("measures a translucent card over a colour from the colour itself", async () => {
    // White text on a half-white card over dark teal: low contrast.
    const page = previewPage({
      color: "rgb(255,255,255)",
      background: "rgba(255,255,255,0.5)",
    });
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "color";
        b.color = "rgb(0,0,0)";
      }),
      noEnvironment,
    );
    expect(result.kind).toBe("measured");
    if (result.kind === "measured") {
      expect(result.ratio).toBeCloseTo(
        contrastRatio(white, { r: 127.5, g: 127.5, b: 127.5 }),
        3,
      );
      expect(result.readable).toBe(false);
    }
  });

  it("is readable once the card is firm enough", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.9)",
    });
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "color";
        b.color = "rgb(0,0,0)";
      }),
      noEnvironment,
    );
    expect(result).toMatchObject({ kind: "measured", readable: true });
  });

  it("falls back to the page's own background when no picture was asked for", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.1)",
    });
    const result = await measureReadability(
      page.doc,
      defaultLoginAppearance,
      noEnvironment,
    );
    expect(result).toMatchObject({ kind: "measured", readable: true });
  });

  it("measures the picture under the card's rectangle", async () => {
    const page = previewPage({
      color: "rgb(255,255,255)",
      background: "rgba(0,0,0,0.2)",
    });
    const image = page.doc.createElement("img");
    image.dataset.current = "";
    image.src = "https://pictures.example/one.jpg";
    const holder = page.doc.createElement("div");
    holder.dataset.loginBackdrop = "pictures";
    holder.append(image);
    page.doc.body.append(holder);
    const sample = vi.fn((_box: unknown, cols: number, rows: number) =>
      grid(cols, rows, () => 255),
    );
    const loadPicture = vi.fn(async () => ({
      width: 1280,
      height: 800,
      sample,
    }));
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "bing";
      }),
      { ...noEnvironment, loadPicture },
    );
    // The page's own element comes too, so a copy fetched with CORS is read in place.
    expect(loadPicture).toHaveBeenCalledWith(
      "https://pictures.example/one.jpg",
      image,
    );
    // The picture is 1280x800 in a 1280x800 window: the box is the card's own.
    expect(sample.mock.calls[0]?.[0]).toEqual({
      x: 100,
      y: 100,
      width: 400,
      height: 300,
    });
    // White picture, 20% black card: bg 204 grey, white text.
    expect(result).toMatchObject({ kind: "measured", readable: false });
  });

  it.each([
    ["a transparent region", "rgb(255, 255, 255)", false],
    ["a transparent region on a dark page", "rgb(10, 10, 10)", true],
  ])(
    "measures %s against the page behind the picture",
    async (_name, pageColor, readable) => {
      const page = previewPage({
        color: "rgb(255,255,255)",
        background: "rgba(0,0,0,0.2)",
      });
      page.doc.body.style.backgroundColor = pageColor;
      const image = page.doc.createElement("img");
      image.dataset.current = "";
      image.src = "https://pictures.example/clear.png";
      const holder = page.doc.createElement("div");
      holder.dataset.loginBackdrop = "pictures";
      holder.append(image);
      page.doc.body.append(holder);
      // A transparent PNG: every pixel black with no opacity.
      const loadPicture = vi.fn(async () => ({
        width: 1280,
        height: 800,
        sample: (_box: unknown, cols: number, rows: number) => ({
          cols,
          rows,
          cells: Array.from({ length: cols * rows }, () => ({
            r: 0,
            g: 0,
            b: 0,
            a: 0,
          })),
        }),
      }));
      const result = await measureReadability(
        page.doc,
        withBackground((b) => {
          b.source = "images";
        }),
        { ...noEnvironment, loadPicture },
      );
      expect(result).toMatchObject({ kind: "measured", readable });
    },
  );

  it("says it cannot measure a picture it cannot read", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.5)",
    });
    const image = page.doc.createElement("img");
    image.dataset.current = "";
    image.src = "https://pictures.example/one.jpg";
    const holder = page.doc.createElement("div");
    holder.dataset.loginBackdrop = "pictures";
    holder.append(image);
    page.doc.body.append(holder);
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "unsplash";
      }),
      noEnvironment,
    );
    expect(result).toEqual({ kind: "unmeasurable", cause: "background" });
  });

  it("says it cannot measure a picture that has not arrived", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.5)",
    });
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "bing";
      }),
      noEnvironment,
    );
    expect(result).toEqual({ kind: "unmeasurable", cause: "background" });
  });

  it("says it cannot measure when the card's colours cannot be read", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.5)",
    });
    const result = await measureReadability(page.doc, defaultLoginAppearance, {
      ...noEnvironment,
      resolveColor: () => undefined,
    });
    expect(result).toEqual({ kind: "unmeasurable", cause: "page" });
  });

  it("says it cannot measure a page with no card", async () => {
    const holder = document.createElement("iframe");
    document.body.append(holder);
    frames.push(holder);
    const doc = holder.contentDocument as Document;
    expect(
      await measureReadability(doc, defaultLoginAppearance, noEnvironment),
    ).toEqual({ kind: "unmeasurable", cause: "page" });
  });

  it("says it cannot measure a colour that is not one yet", async () => {
    const page = previewPage({
      color: "rgb(0,0,0)",
      background: "rgba(255,255,255,0.5)",
    });
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "color";
        b.color = "#1a";
      }),
      noEnvironment,
    );
    expect(result).toEqual({ kind: "unmeasurable", cause: "background" });
  });

  it("does not drop a frame whose colour cannot be read", async () => {
    const page = previewPage(
      { color: "rgb(0,0,0)", background: "rgba(255,255,255,0.5)" },
      { background: "rgb(1,2,3)" },
    );
    const result = await measureReadability(page.doc, defaultLoginAppearance, {
      ...noEnvironment,
      resolveColor: (css) =>
        css.replaceAll(" ", "").startsWith("rgb(1,2,3")
          ? undefined
          : parseColor(css.replaceAll(" ", "")),
    });
    expect(result).toEqual({ kind: "unmeasurable", cause: "page" });
  });

  it("puts the frame between the backdrop and the card", async () => {
    const page = previewPage(
      { color: "rgb(255,255,255)", background: "rgba(255,255,255,0.1)" },
      { background: "rgb(1,2,3)" },
    );
    const result = await measureReadability(
      page.doc,
      withBackground((b) => {
        b.source = "color";
        b.color = "rgb(255,255,255)";
      }),
      noEnvironment,
    );
    // A black frame under a nearly clear card keeps white text readable.
    expect(result).toMatchObject({ kind: "measured", readable: true });
  });
});

describe("noticing that the preview changed", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("reports once, after a pause, however many things change", async () => {
    const page = previewPage({ color: "black", background: "white" });
    const onChange = vi.fn();
    const stop = watchPreview(page.doc, onChange, 100);
    page.card.style.backgroundColor = "rgba(255,255,255,0.5)";
    page.card.setAttribute("data-position", "left");
    await vi.advanceTimersByTimeAsync(0);
    expect(onChange).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(150);
    expect(onChange).toHaveBeenCalledTimes(1);
    stop();
  });

  it.each([
    [
      "the theme",
      (page: Page) =>
        page.doc.documentElement.setAttribute("data-theme", "dark"),
    ],
    [
      "a picture arriving",
      (page: Page) => page.doc.body.append(page.doc.createElement("img")),
    ],
    [
      "a picture loading",
      (page: Page) => page.doc.dispatchEvent(new Event("load")),
    ],
    [
      "the window size",
      (page: Page) => page.doc.defaultView?.dispatchEvent(new Event("resize")),
    ],
  ])("notices %s", async (_name, change) => {
    const page = previewPage({ color: "black", background: "white" });
    const onChange = vi.fn();
    const stop = watchPreview(page.doc, onChange, 100);
    change(page);
    await vi.advanceTimersByTimeAsync(150);
    expect(onChange).toHaveBeenCalledTimes(1);
    stop();
  });

  it("stops when told to", async () => {
    const page = previewPage({ color: "black", background: "white" });
    const onChange = vi.fn();
    const stop = watchPreview(page.doc, onChange, 100);
    page.doc.documentElement.setAttribute("data-theme", "dark");
    stop();
    await vi.advanceTimersByTimeAsync(300);
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("the caches", () => {
  it("keep the newest entries and drop the oldest past their size", () => {
    const cache = new Map<string, number>();
    for (const [i, key] of ["a", "b", "c"].entries())
      remember(cache, key, i, 2);
    expect([...cache.keys()]).toEqual(["b", "c"]);
    remember(cache, "b", 9, 2);
    remember(cache, "d", 3, 2);
    expect([...cache.keys()]).toEqual(["b", "d"]);
  });
});

describe("reading a colour with the browser", () => {
  it("refuses text that is not a colour and reads one that is", () => {
    const context = {
      canvas: { width: 0, height: 0 },
      fillStyle: "",
      clearRect: () => undefined,
      fillRect: () => undefined,
      getImageData: () => ({ data: [10, 20, 30, 255] }),
    };
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(
      context as unknown as CanvasRenderingContext2D,
    );
    expect(canvasEnvironment.resolveColor("#1a")).toBeUndefined();
    expect(canvasEnvironment.resolveColor("#0a141e")).toEqual({
      r: 10,
      g: 20,
      b: 30,
      a: 1,
    });
  });
});
