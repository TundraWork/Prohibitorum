import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  loginWallpaperQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import type { LoginAppearance, PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { PublicLayout } from "@/components/custom/PublicLayout";
import { i18n } from "@/i18n";

const config: PublicConfig = {
  instanceName: "Test instance",
  hasCustomIcon: true,
  iconUrl: "/branding/icon",
  iconEtag: "abc",
  maintenanceMode: false,
  maintenanceMessage: "",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Test", algorithm: "SHA1", digits: 6, period: 30 },
};
let queryClient: QueryClient;
let notifyError: ReturnType<typeof vi.fn<(error: unknown) => void>>;

/**
 * jsdom never loads an image, and the avatar draws its image only once the
 * browser has, so this one reports every picture as loaded.
 */
class LoadedImage extends EventTarget {
  complete = true;
  naturalWidth = 1;
  src = "";
}

beforeEach(() => {
  vi.stubGlobal("Image", LoadedImage);
  i18n.activate("en");
  notifyError = vi.fn<(error: unknown) => void>();
  queryClient = createQueryClient(notifyError);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
});

function withAppearance(edit: (appearance: LoginAppearance) => void) {
  const appearance = structuredClone(defaultLoginAppearance);
  edit(appearance);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
    ...config,
    loginAppearance: appearance,
  });
}
afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

async function mount() {
  const root = createRootRoute({ component: PublicLayout });
  const page = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <h1>Page</h1>,
  });
  const router = createRouter({
    routeTree: root.addChildren([page]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  await screen.findByRole("heading", { name: "Page" });
  return screen.getByRole("banner");
}

it("names the instance with its icon in the toolbar, beside the language and theme controls", async () => {
  const banner = await mount();
  expect(within(banner).getByText("Test instance")).toBeVisible();
  const icon = banner.querySelector("img");
  expect(icon).toHaveAttribute("src", "/branding/icon?v=abc");
  expect(icon).toHaveAttribute("alt", "");
  expect(
    within(banner).getByRole("button", { name: "Language" }),
  ).toBeVisible();
  expect(
    within(banner).getByRole("radiogroup", { name: "Theme" }),
  ).toBeVisible();
});

it("puts the page on the card below the toolbar", async () => {
  const banner = await mount();
  const main = screen.getByRole("main");
  expect(within(main).getByRole("heading", { name: "Page" })).toBeVisible();
  expect(
    banner.compareDocumentPosition(main) & Node.DOCUMENT_POSITION_FOLLOWING,
  ).toBeTruthy();
});

it("rounds every control inside the toolbar's capsules and draws them translucent and frosted by default", async () => {
  const banner = await mount();
  const capsules = banner.querySelectorAll<HTMLElement>(
    "[data-toolbar-capsule]",
  );
  expect(capsules).toHaveLength(2);
  for (const capsule of capsules) {
    expect(capsule).toHaveClass("[--field-radius:100%]", "[--radius:100%]");
    expect(capsule.style.getPropertyValue("--surface-alpha")).toBe("70%");
    expect(capsule).toHaveClass("backdrop-blur-xl");
  }
});

it("draws an opaque toolbar on the surface colour", async () => {
  withAppearance((appearance) => {
    appearance.capsules.translucent = false;
  });
  const banner = await mount();
  for (const capsule of banner.querySelectorAll<HTMLElement>(
    "[data-toolbar-capsule]",
  )) {
    expect(capsule).toHaveClass("bg-surface");
    expect(capsule.style.getPropertyValue("--surface-alpha")).toBe("");
    expect(capsule).not.toHaveClass("backdrop-blur-xl");
  }
});

it("makes the card translucent only when the settings say so", async () => {
  await mount();
  const opaque = screen.getByRole("main").querySelector<HTMLElement>(".card");
  expect(opaque?.style.getPropertyValue("--surface-alpha")).toBe("");

  queryClient.clear();
  withAppearance((appearance) => {
    appearance.card = { translucent: true, opacity: 60, blur: false };
  });
  document.body.innerHTML = "";
  await mount();
  const card = screen.getByRole("main").querySelector<HTMLElement>(".card");
  expect(card?.style.getPropertyValue("--surface-alpha")).toBe("60%");
  expect(card).not.toHaveClass("backdrop-blur-xl");
});

it("keeps its own background and says nothing when the wallpaper cannot be read", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(JSON.stringify({ code: "wallpaper_unavailable" }), {
          status: 503,
          headers: { "Content-Type": "application/json" },
        }),
    ),
  );
  withAppearance((appearance) => {
    appearance.background.source = "bing";
  });
  await mount();
  await vi.waitFor(() =>
    expect(
      queryClient.getQueryState(
        loginWallpaperQueryOptions(
          (
            queryClient.getQueryData(
              publicConfigQueryOptions().queryKey,
            ) as PublicConfig
          ).loginAppearance.background,
        ).queryKey,
      )?.status,
    ).toBe("error"),
  );
  expect(document.querySelector("[data-login-backdrop]")).toBeNull();
  expect(notifyError).not.toHaveBeenCalled();
});

it("shows the Bing picture and its caption once the wallpaper arrives", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            source: "bing",
            imageUrl: "https://www.bing.com/th?id=OHR.X_UHD.jpg&w=2560",
            title: "A quiet ridge",
            copyright: "Somewhere (© Someone)",
            copyrightUrl: "https://www.bing.com/search?q=x",
          }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        ),
    ),
  );
  withAppearance((appearance) => {
    appearance.background.source = "bing";
  });
  await mount();
  const photo = await vi.waitFor(() => {
    const img = document.querySelector('[data-login-backdrop="photo"]');
    if (!img) throw new Error("no photo yet");
    return img;
  });
  expect(photo).toHaveAttribute(
    "src",
    "https://www.bing.com/th?id=OHR.X_UHD.jpg&w=2560",
  );
  expect(photo).toHaveAttribute("alt", "");
  expect(screen.getAllByText("A quiet ridge").length).toBeGreaterThan(0);
  expect(
    screen.getAllByRole("link", { name: "Somewhere (© Someone)" })[0],
  ).toHaveAttribute("href", "https://www.bing.com/search?q=x");
});
