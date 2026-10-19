import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GroupApplicationView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupUsedBySection } from "@/pages/admin/groups/GroupUsedBySection";
import { apiError } from "@/test/app";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

async function mount(response: () => Response | Promise<Response>) {
  fetchBoundary.mockImplementation(async () => response());
  const root = createRootRoute({
    component: () => <GroupUsedBySection groupId={7} />,
  });
  // The rows link to the application pages, so those routes have to exist.
  const pages = [
    "/admin/oidc-applications/$clientId",
    "/admin/saml-applications/$id",
    "/admin/forward-auth-apps/$clientId",
  ].map((path) =>
    createRoute({ getParentRoute: () => root, path, component: () => null }),
  );
  const router = createRouter({
    routeTree: root.addChildren(pages),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  await screen.findByRole("heading", { level: 2, name: "Used by" });
}

const page = (items: GroupApplicationView[]) => () =>
  Response.json({ items, nextCursor: "" });

describe("the used-by section", () => {
  it("says no application uses the group when none does", async () => {
    await mount(page([]));

    expect(
      await screen.findByText("No application uses this group."),
    ).toBeVisible();
    expect(screen.queryByRole("list")).toBeNull();
  });

  it("lists each application with its kind, linking to its own page", async () => {
    await mount(
      page([
        { kind: "oidc", appId: "grafana", displayName: "Grafana" },
        { kind: "saml", appId: "3", displayName: "Company wiki" },
        { kind: "forward_auth", appId: "jellyfin", displayName: "Jellyfin" },
      ]),
    );

    const list = await screen.findByRole("list", { name: "Applications" });
    expect(within(list).getAllByRole("listitem")).toHaveLength(3);
    const grafana = within(list).getByRole("link", { name: /Grafana/ });
    expect(grafana).toHaveAttribute("href", "/admin/oidc-applications/grafana");
    expect(grafana).toHaveTextContent("OpenID Connect");
    const wiki = within(list).getByRole("link", { name: /Company wiki/ });
    expect(wiki).toHaveAttribute("href", "/admin/saml-applications/3");
    expect(wiki).toHaveTextContent("SAML");
    const jellyfin = within(list).getByRole("link", { name: /Jellyfin/ });
    expect(jellyfin).toHaveAttribute(
      "href",
      "/admin/forward-auth-apps/jellyfin",
    );
    expect(jellyfin).toHaveTextContent("Forward-auth");
  });

  it("tells a failed read in the section, with a retry, and no toast", async () => {
    await mount(() => apiError("internal_error", 500));

    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
    await waitFor(() => expect(notifyError).not.toHaveBeenCalled());
  });
});
