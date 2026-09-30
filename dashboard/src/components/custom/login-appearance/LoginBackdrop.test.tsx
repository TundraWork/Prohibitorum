import { act, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { LoginAppearance, Wallpaper } from "@/api/raw-paths";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { gradientBackground } from "@/components/custom/login-appearance/gradients";
import { LoginBackdrop } from "@/components/custom/login-appearance/LoginBackdrop";

/** jsdom has no image decoder; every picture decodes at once. */
class DecodingImage {
  src = "";
  decode() {
    return Promise.resolve();
  }
}

function appearance(edit: (a: LoginAppearance) => void): LoginAppearance {
  const a = structuredClone(defaultLoginAppearance);
  edit(a);
  return a;
}

function reducedMotion(on: boolean) {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({
        matches: on && query.includes("prefers-reduced-motion"),
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }) as MediaQueryList,
  );
}

const images = ["/i/1", "/i/2", "/i/3"];

function currentSrc(container: HTMLElement) {
  return container.querySelector("img[data-current]")?.getAttribute("src");
}

beforeEach(() => {
  vi.stubGlobal("Image", DecodingImage);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("each source", () => {
  it("draws nothing for the page's own background", () => {
    const { container } = render(
      <LoginBackdrop appearance={defaultLoginAppearance} images={images} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("fills the window with a colour, behind everything", () => {
    const { container } = render(
      <LoginBackdrop
        appearance={appearance((a) => {
          a.background.source = "color";
          a.background.color = "#24324a";
        })}
        images={[]}
      />,
    );
    const layer = container.querySelector<HTMLElement>("[data-login-backdrop]");
    expect(layer).toHaveAttribute("data-login-backdrop", "color");
    expect(layer).toHaveClass("fixed", "inset-0", "-z-10");
    expect(layer).toHaveAttribute("aria-hidden", "true");
    expect(layer?.style.backgroundColor).toBe("rgb(36, 50, 74)");
  });

  it("draws the chosen gradient, and fills its container when contained", () => {
    const { container } = render(
      <LoginBackdrop
        contained
        appearance={appearance((a) => {
          a.background.source = "gradient";
          a.background.gradient = "aurora";
        })}
        images={[]}
      />,
    );
    const layer = container.querySelector<HTMLElement>("[data-login-backdrop]");
    expect(layer).toHaveClass("absolute", "inset-0");
    expect(layer).not.toHaveClass("fixed");
    // jsdom cannot parse oklch(), so the preset is read off the layer.
    expect(layer).toHaveAttribute("data-gradient", "aurora");
    expect(gradientBackground.aurora).toContain("oklch(24% 0.04 250)");
  });

  it("draws a Bing or Unsplash picture only once it has arrived for that source", () => {
    const bing = appearance((a) => {
      a.background.source = "bing";
    });
    const wallpaper: Wallpaper = {
      source: "bing",
      imageUrl: "https://www.bing.com/th?id=OHR.X_UHD.jpg&w=2560",
    };
    const { container, rerender } = render(
      <LoginBackdrop appearance={bing} images={[]} />,
    );
    expect(container).toBeEmptyDOMElement();

    rerender(
      <LoginBackdrop appearance={bing} images={[]} wallpaper={wallpaper} />,
    );
    const photo = container.querySelector("img");
    expect(photo).toHaveAttribute("src", wallpaper.imageUrl);
    expect(photo).toHaveAttribute("alt", "");
    expect(photo).toHaveAttribute("aria-hidden", "true");
    expect(photo).toHaveClass("opacity-0", "transition-opacity");

    // A wallpaper read for another source is not drawn.
    rerender(
      <LoginBackdrop
        appearance={appearance((a) => {
          a.background.source = "unsplash";
        })}
        images={[]}
        wallpaper={wallpaper}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("draws nothing for images when none are uploaded", () => {
    const { container } = render(
      <LoginBackdrop
        appearance={appearance((a) => {
          a.background.source = "images";
        })}
        images={[]}
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

describe("uploaded images", () => {
  it("keeps the random pick for as long as the page is open", () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0.5);
    const random = appearance((a) => {
      a.background.source = "images";
      a.background.images = { order: "random", intervalSeconds: 5 };
    });
    const { container, rerender } = render(
      <LoginBackdrop appearance={random} images={images} />,
    );
    expect(currentSrc(container)).toBe("/i/2");
    vi.mocked(Math.random).mockReturnValue(0);
    act(() => {
      vi.advanceTimersByTime(60_000);
    });
    rerender(<LoginBackdrop appearance={random} images={[...images]} />);
    expect(currentSrc(container)).toBe("/i/2");
    expect(container.querySelectorAll("img")).toHaveLength(1);
  });

  it("moves to the next image every interval and fades it in over the last", async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const carousel = appearance((a) => {
      a.background.source = "images";
      a.background.images = { order: "carousel", intervalSeconds: 15 };
    });
    const { container } = render(
      <LoginBackdrop appearance={carousel} images={images} />,
    );
    expect(currentSrc(container)).toBe("/i/1");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(14_000);
    });
    expect(currentSrc(container)).toBe("/i/1");

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1_000);
    });
    expect(currentSrc(container)).toBe("/i/2");
    const current = container.querySelector("img[data-current]");
    expect(current).toHaveClass("transition-opacity", "duration-1000");
    // The previous picture stays underneath while the new one fades in.
    expect(container.querySelectorAll("img")).toHaveLength(2);

    // Each picture gets a whole interval, so the timer restarts after a change.
    for (const next of ["/i/3", "/i/1"]) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15_000);
      });
      expect(currentSrc(container)).toBe(next);
    }
  });

  it("swaps pictures without a fade when the reader asks for less motion", async () => {
    reducedMotion(true);
    vi.useFakeTimers();
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { container } = render(
      <LoginBackdrop
        appearance={appearance((a) => {
          a.background.source = "images";
          a.background.images = { order: "carousel", intervalSeconds: 5 };
        })}
        images={images}
      />,
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });
    expect(currentSrc(container)).toBe("/i/2");
    for (const img of container.querySelectorAll("img")) {
      expect(img.className).not.toContain("transition");
    }
    expect(container.querySelectorAll("img")).toHaveLength(1);
  });

  it("draws a Bing picture without a fade when the reader asks for less motion", () => {
    reducedMotion(true);
    const { container } = render(
      <LoginBackdrop
        appearance={appearance((a) => {
          a.background.source = "bing";
        })}
        images={[]}
        wallpaper={{ source: "bing", imageUrl: "https://www.bing.com/th?id=x" }}
      />,
    );
    expect(container.querySelector("img")?.className).not.toContain(
      "transition",
    );
  });
});
