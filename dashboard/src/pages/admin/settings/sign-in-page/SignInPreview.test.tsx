import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { act, render, screen, waitFor } from "@testing-library/react";
import { createContext, useContext, useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
  publicFederationProvidersQueryOptions,
} from "@/api/queries";
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
  queryClient.setQueryData(authStatusQueryOptions().queryKey, {
    bootstrapped: true,
  });
  queryClient.setQueryData(publicFederationProvidersQueryOptions().queryKey, [
    {
      slug: "github",
      displayName: "GitHub",
      kind: "oidc",
      allowSignIn: true,
      iconUrl: null,
    },
  ]);
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

type PreviewProps = React.ComponentProps<typeof SignInPreview>;
const Props = createContext<PreviewProps | undefined>(undefined);

function Preview() {
  const props = useContext(Props) as PreviewProps;
  return <SignInPreview {...props} />;
}

/** The preview inside a router, as the console draws it, so the sign-in page it shows has one too. */
function Harness({ props }: { props: PreviewProps }) {
  const [router] = useState(() =>
    createRouter({
      routeTree: createRootRoute({ component: Preview }),
      history: createMemoryHistory({ initialEntries: ["/"] }),
    }),
  );
  return (
    <Props value={props}>
      <RouterProvider router={router} />
    </Props>
  );
}

function view(props: Partial<PreviewProps> = {}) {
  return (
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Harness
          props={{
            appearance: defaultLoginAppearance,
            images: [],
            hasUnsplashKey: false,
            theme: "light",
            size: "desktop",
            ...props,
          }}
        />
      </QueryClientProvider>
    </I18nProvider>
  );
}

/** Renders the preview inside its router and waits for its frame. */
async function show(ui: React.ReactElement) {
  const result = render(ui);
  await waitFor(() =>
    expect(
      document.querySelector("iframe[data-sign-in-preview]"),
    ).not.toBeNull(),
  );
  return result;
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
    await show(view());
    const page = await pageDocument();
    expect(page.querySelector('[data-slot="stage-card"]')).not.toBeNull();
    expect(page.querySelector("header")).not.toBeNull();
    expect(page.body).toHaveTextContent("Sign in with Test instance");
    expect(page.body).toHaveTextContent("Sign in with a passkey");
    expect(page.body).toHaveTextContent("or with your password");
    expect(page.body).toHaveTextContent("or continue with");
    expect(page.body).toHaveTextContent("GitHub");
    expect(page.body).toHaveTextContent(
      "Sign in with your phone or another device",
    );
    expect(page.body).toHaveTextContent("Stay signed in on this device");
  });

  it("leaves focus in the console while it is drawn", async () => {
    await show(view());
    const page = await pageDocument();
    expect(page.querySelector("h1")).not.toBeNull();
    expect(page.activeElement).toBe(page.body);
  });

  it("never waits for a passkey while it is only a picture", async () => {
    const conditional = vi.fn();
    vi.stubGlobal("PublicKeyCredential", {
      isConditionalMediationAvailable: conditional,
    });
    await show(view());
    await pageDocument();
    expect(conditional).not.toHaveBeenCalled();
  });

  it("keeps the frame out of the page's tab order and reading order", async () => {
    await show(view());
    await pageDocument();
    const frame = document.querySelector("iframe[data-sign-in-preview]");
    expect(frame).toHaveAttribute("inert");
    expect(frame).toHaveAttribute("tabindex", "-1");
  });

  it("draws the draft's background and follows it as it changes", async () => {
    const { rerender } = await show(view());
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
    await show(
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

  it("fetches its picture so it can be read, and draws it the ordinary way if refused", async () => {
    await show(
      view({
        appearance: withAppearance((a) => {
          a.background.source = "images";
        }),
        images: ["https://pictures.example/one.jpg"],
      }),
    );
    const page = await pageDocument();
    const image = () =>
      page.querySelector<HTMLImageElement>(
        '[data-login-backdrop="pictures"] img',
      ) as HTMLImageElement;
    expect(image()).toHaveAttribute("crossorigin", "anonymous");

    act(() => {
      image().dispatchEvent(new Event("error"));
    });
    await waitFor(() => expect(image()).not.toHaveAttribute("crossorigin"));
  });

  it.each([
    ["left", "left"],
    ["center", "center"],
    ["right", "right"],
  ] as const)("places the card at the %s", async (position, expected) => {
    await show(
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
    const { rerender } = await show(view({ theme: "dark" }));
    const page = await pageDocument();
    expect(page.documentElement).toHaveClass("dark");
    expect(document.documentElement).not.toHaveClass("dark");

    rerender(view({ theme: "light" }));
    await waitFor(() => expect(page.documentElement).not.toHaveClass("dark"));
  });

  it("lays the page out at desktop or phone width", async () => {
    const { rerender } = await show(view());
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
    const { rerender } = await show(view());
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

  it("says an Unsplash photo needs a saved access key", async () => {
    await show(
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
    await show(
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
    await show(
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
