import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { publicConfigQueryOptions } from "@/api/queries";
import type { LoginAppearance } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";
import { SignInPreview } from "@/pages/admin/settings/sign-in-page/SignInPreview";
import { apiError, fakeApi, testConfig } from "@/test/app";

let queryClient: QueryClient;
let notifyError: ReturnType<typeof vi.fn<(error: unknown) => void>>;

beforeEach(() => {
  i18n.activate("en");
  notifyError = vi.fn<(error: unknown) => void>();
  queryClient = createQueryClient(notifyError);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, testConfig);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function withAppearance(edit: (a: LoginAppearance) => void): LoginAppearance {
  const a = structuredClone(defaultLoginAppearance);
  edit(a);
  return a;
}

function view(props: Partial<React.ComponentProps<typeof SignInPreview>> = {}) {
  return (
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <SignInPreview
          appearance={defaultLoginAppearance}
          images={[]}
          hasUnsplashKey={false}
          theme="light"
          size="desktop"
          {...props}
        />
      </QueryClientProvider>
    </I18nProvider>
  );
}

/** The sign-in page's own document, which the preview draws in a frame. */
async function pageDocument(): Promise<Document> {
  const frame = document.querySelector<HTMLIFrameElement>(
    "iframe[data-sign-in-preview]",
  );
  if (!frame) throw new Error("no preview frame");
  await waitFor(() => {
    if (!frame.contentDocument?.querySelector("main"))
      throw new Error("the page is not drawn yet");
  });
  return frame.contentDocument as Document;
}

describe("the sign-in page preview", () => {
  it("draws the sign-in page itself, in a frame of its own", async () => {
    render(view());
    const page = await pageDocument();
    expect(page.querySelector('[data-slot="stage-card"]')).not.toBeNull();
    expect(page.querySelector("header")).not.toBeNull();
    expect(page.body).toHaveTextContent("Sign in");
    expect(page.body).toHaveTextContent("Sign in with a passkey");
    expect(page.body).toHaveTextContent("Test instance");
  });

  it("keeps the frame out of the page's tab order and reading order", async () => {
    render(view());
    await pageDocument();
    const frame = document.querySelector("iframe[data-sign-in-preview]");
    expect(frame).toHaveAttribute("inert");
    expect(frame).toHaveAttribute("tabindex", "-1");
  });

  it("draws the draft's background and follows it as it changes", async () => {
    const { rerender } = render(view());
    const page = await pageDocument();
    expect(page.querySelector("[data-login-backdrop]")).toBeNull();

    rerender(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "gradient";
          a.background.gradient = "ember";
        }),
      }),
    );
    await waitFor(() =>
      expect(
        page.querySelector('[data-login-backdrop="gradient"]'),
      ).toHaveAttribute("data-gradient", "ember"),
    );
  });

  it("draws uploaded images behind the page", async () => {
    render(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "images";
        }),
        images: ["/branding/login-images/1"],
      }),
    );
    const page = await pageDocument();
    expect(
      page.querySelector('[data-login-backdrop="pictures"] img'),
    ).toHaveAttribute("src", "/branding/login-images/1");
  });

  it.each([
    ["left", "left"],
    ["center", "center"],
    ["right", "right"],
  ] as const)("places the card at the %s", async (position, expected) => {
    render(
      view({
        appearance: withAppearance((a) => {
          a.cardPosition = position;
        }),
      }),
    );
    const page = await pageDocument();
    expect(page.querySelector("main")).toHaveAttribute(
      "data-position",
      expected,
    );
  });

  it("applies the chosen theme to the page, not to the console", async () => {
    const { rerender } = render(view({ theme: "dark" }));
    const page = await pageDocument();
    expect(page.documentElement).toHaveClass("dark");
    expect(document.documentElement).not.toHaveClass("dark");

    rerender(view({ theme: "light" }));
    await waitFor(() => expect(page.documentElement).not.toHaveClass("dark"));
  });

  it("lays the page out at desktop or phone width", async () => {
    const { rerender } = render(view());
    await pageDocument();
    const frame = () =>
      document.querySelector<HTMLIFrameElement>("iframe[data-sign-in-preview]");
    expect(frame()?.style.width).toBe("1280px");
    expect(frame()?.style.height).toBe("800px");

    rerender(view({ size: "phone" }));
    expect(frame()?.style.width).toBe("390px");
    expect(frame()?.style.height).toBe("844px");
  });

  it("draws the toolbar's theme control only while visitors choose the theme", async () => {
    const { rerender } = render(view());
    const page = await pageDocument();
    expect(
      page.querySelector('header button[aria-label="Theme"]'),
    ).not.toBeNull();

    rerender(
      view({
        appearance: withAppearance((a) => {
          a.theme = "dark";
        }),
      }),
    );
    await waitFor(() =>
      expect(
        page.querySelector('header button[aria-label="Theme"]'),
      ).toBeNull(),
    );
  });

  it("says an Unsplash photo needs a saved access key", () => {
    render(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "unsplash";
        }),
      }),
    );
    expect(
      screen.getByText("Save an access key to preview Unsplash photos."),
    ).toBeInTheDocument();
  });

  it("says when Bing's picture cannot be read, without a toast", async () => {
    fakeApi({
      "GET /api/prohibitorum/admin/settings/login-appearance/wallpaper": () =>
        apiError("wallpaper_unavailable", 503),
    });
    render(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "bing";
        }),
      }),
    );
    expect(
      await screen.findByText("Couldn't get the picture from Bing."),
    ).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("shows the picture Bing sent for the draft's region", async () => {
    const requests = fakeApi({
      "GET /api/prohibitorum/admin/settings/login-appearance/wallpaper": () =>
        Response.json({
          source: "bing",
          pictures: [
            {
              imageUrl: "https://www.bing.com/th?id=OHR.X_UHD.jpg",
              title: "A quiet ridge",
              copyright: "Somewhere",
              copyrightUrl: "https://www.bing.com/search?q=x",
            },
          ],
        }),
    });
    render(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "bing";
          a.background.bing.market = "en-GB";
        }),
      }),
    );
    const page = await pageDocument();
    await waitFor(() =>
      expect(page.querySelector("[data-login-backdrop] img")).toHaveAttribute(
        "src",
        "https://www.bing.com/th?id=OHR.X_UHD.jpg",
      ),
    );
    expect(page.body).toHaveTextContent("A quiet ridge");
    const sent = requests.sent(
      "GET",
      "/api/prohibitorum/admin/settings/login-appearance/wallpaper",
    );
    expect(sent[0]?.url.searchParams.get("market")).toBe("en-GB");
  });
});
