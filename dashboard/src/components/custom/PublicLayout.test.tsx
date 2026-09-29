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
import { publicConfigQueryOptions } from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { PublicLayout } from "@/components/custom/PublicLayout";
import { i18n } from "@/i18n";

const config: PublicConfig = {
  instanceName: "Test instance",
  hasCustomIcon: true,
  iconUrl: "/branding/icon",
  iconEtag: "abc",
  maintenanceMode: false,
  maintenanceMessage: "",
  hasCustomBackground: false,
  backgroundUrl: "",
  backgroundEtag: "",
  totp: { issuer: "Test", algorithm: "SHA1", digits: 6, period: 30 },
};
let queryClient: QueryClient;

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
  queryClient = createQueryClient(() => {});
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
});
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
