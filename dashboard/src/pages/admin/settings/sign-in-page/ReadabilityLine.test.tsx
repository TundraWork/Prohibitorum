import { I18nProvider } from "@lingui/react";
import {
  act,
  render as plainRender,
  renderHook,
  screen,
} from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginAppearance } from "@/api/raw-paths";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import {
  ReadabilityLine,
  useReadability,
} from "@/pages/admin/settings/sign-in-page/ReadabilityLine";
import type { Environment } from "@/pages/admin/settings/sign-in-page/readability";

beforeEach(() => {
  i18n.activate("en");
});

const render = (ui: ReactElement) =>
  plainRender(<I18nProvider i18n={i18n}>{ui}</I18nProvider>);

describe("the readability line", () => {
  it("calls a firm card readable and gives the ratio", () => {
    render(
      <ReadabilityLine
        result={{ kind: "measured", ratio: 9.46, readable: true }}
      />,
    );
    const line = screen.getByRole("status");
    expect(line).toHaveTextContent("Readable");
    expect(line).toHaveTextContent("Body text contrast 9.4 : 1");
    expect(line).not.toHaveTextContent("Hard to read");
  });

  it("warns, and says what to change, below the line", () => {
    render(
      <ReadabilityLine
        result={{ kind: "measured", ratio: 3.27, readable: false }}
      />,
    );
    const line = screen.getByRole("status");
    expect(line).toHaveTextContent("Hard to read");
    expect(line).toHaveTextContent(
      "Body text contrast 3.2 : 1 — increase the card opacity or choose another background",
    );
  });

  it("never shows a ratio just under the line as on it", () => {
    render(
      <ReadabilityLine
        result={{ kind: "measured", ratio: 4.49, readable: false }}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Body text contrast 4.4 : 1",
    );
  });

  it("says plainly when it cannot measure", () => {
    render(
      <ReadabilityLine
        result={{ kind: "unmeasurable", cause: "background" }}
      />,
    );
    const line = screen.getByRole("status");
    expect(line).toHaveTextContent(
      "Readability can't be measured for this background.",
    );
    expect(line).not.toHaveTextContent("Readable");
    expect(line).not.toHaveTextContent("contrast");
    expect(line).toHaveTextContent("Not measured");
  });

  it("does not blame the background when the page could not be measured", () => {
    render(
      <ReadabilityLine result={{ kind: "unmeasurable", cause: "page" }} />,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "Readability can't be measured right now.",
    );
    i18n.activate("zh");
    plainRender(
      <I18nProvider i18n={i18n}>
        <ReadabilityLine result={{ kind: "unmeasurable", cause: "page" }} />
      </I18nProvider>,
    );
    expect(screen.getAllByRole("status")[1]).toHaveTextContent(
      "暂时无法测量可读性。",
    );
  });

  it("is worded in Chinese too", () => {
    i18n.activate("zh");
    render(
      <ReadabilityLine
        result={{ kind: "measured", ratio: 3.27, readable: false }}
      />,
    );
    expect(screen.getByRole("status")).toHaveTextContent(
      "正文对比度 3.2 : 1，请提高卡片不透明度或更换背景",
    );
  });
});

/* The hook measures the real preview document; only colours and pictures are stood in. */

const frames: HTMLIFrameElement[] = [];
afterEach(() => {
  for (const frame of frames.splice(0)) frame.remove();
  vi.useRealTimers();
});

function preview() {
  const holder = document.createElement("iframe");
  document.body.append(holder);
  frames.push(holder);
  const doc = holder.contentDocument as Document;
  const card = doc.createElement("div");
  card.dataset.slot = "stage-card";
  card.style.color = "rgb(0,0,0)";
  card.style.backgroundColor = "rgba(255,255,255,0.5)";
  doc.body.append(card);
  return { doc, card };
}

const environment: Environment = {
  resolveColor: (css) => {
    const m = /^rgba?\(([^)]*)\)$/.exec(css.replaceAll(" ", ""));
    if (!m?.[1]) return undefined;
    const [r = 0, g = 0, b = 0, a = 1] = m[1].split(",").map(Number);
    return { r, g, b, a };
  },
  loadPicture: async () => undefined,
};

function colored(color: string): LoginAppearance {
  const appearance = structuredClone(defaultLoginAppearance);
  appearance.background.source = "color";
  appearance.background.color = color;
  return appearance;
}

describe("keeping the verdict current", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it("says it cannot measure when the measurement itself fails", async () => {
    const { doc } = preview();
    const failing: Environment = {
      ...environment,
      resolveColor: () => {
        throw new Error("no canvas");
      },
    };
    const { result } = renderHook(() =>
      useReadability(doc, colored("rgb(255,255,255)"), failing),
    );
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(result.current).toEqual({ kind: "unmeasurable", cause: "page" });
  });

  it("says nothing without a preview", async () => {
    const { result } = renderHook(() =>
      useReadability(null, colored("rgb(0,0,0)"), environment),
    );
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(result.current).toBeUndefined();
  });

  it("measures the preview shortly after it appears", async () => {
    const { doc } = preview();
    const { result } = renderHook(() =>
      useReadability(doc, colored("rgb(255,255,255)"), environment),
    );
    expect(result.current).toBeUndefined();
    await act(() => vi.advanceTimersByTimeAsync(500));
    // Black text on half-white over white.
    expect(result.current).toMatchObject({ kind: "measured", readable: true });
  });

  it("measures again when the background changes", async () => {
    const { doc, card } = preview();
    card.style.color = "rgb(255,255,255)";
    const { result, rerender } = renderHook(
      ({ appearance }) => useReadability(doc, appearance, environment),
      { initialProps: { appearance: colored("rgb(255,255,255)") } },
    );
    await act(() => vi.advanceTimersByTimeAsync(500));
    const onWhite = result.current;
    expect(onWhite).toMatchObject({ kind: "measured", readable: false });
    rerender({ appearance: colored("rgb(0,0,0)") });
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(result.current).not.toEqual(onWhite);
    expect(result.current).toMatchObject({ kind: "measured" });
  });

  it("measures again when the card's surface or the theme changes in the preview", async () => {
    const { doc, card } = preview();
    card.style.color = "rgb(255,255,255)";
    const { result } = renderHook(() =>
      useReadability(doc, colored("rgb(0,0,0)"), environment),
    );
    await act(() => vi.advanceTimersByTimeAsync(500));
    const before = result.current;
    card.style.backgroundColor = "rgba(255,255,255,0.1)";
    doc.documentElement.setAttribute("data-theme", "dark");
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(result.current).not.toEqual(before);
  });

  it("does not answer after the preview is gone", async () => {
    const { doc } = preview();
    const { result, rerender } = renderHook(
      ({ target }: { target: Document | null }) =>
        useReadability(target, colored("rgb(255,255,255)"), environment),
      { initialProps: { target: doc as Document | null } },
    );
    await act(() => vi.advanceTimersByTimeAsync(500));
    expect(result.current).toBeDefined();
    rerender({ target: null });
    expect(result.current).toBeUndefined();
  });
});
