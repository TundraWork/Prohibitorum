import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { consentQueryOptions, sessionQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ConnectedApps } from "@/pages/ConnectedApps";
import { Profile } from "@/pages/Profile";

vi.mock("qrcode", () => ({
  default: { toCanvas: vi.fn().mockResolvedValue(undefined) },
}));

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const history = createBrowserHistory();
let queryClient: QueryClient;

type Session = components["schemas"]["SessionView"];

const session: Session = {
  id: 1,
  username: "alice",
  displayName: "Alice",
  role: "user",
  avatarSource: "user",
  avatarSourceLabels: { "upstream:github": "GitHub" },
  avatarSourceUrls: { user: "/api/x/avatar?v=1", "upstream:github": "/u.jpg" },
};

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined);
  queryClient.setQueryData(sessionQueryOptions().queryKey, session);
  queryClient.setQueryData(consentQueryOptions().queryKey, []);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

/**
 * Mounts one page under a router, the way the real route file mounts it.
 *
 * None of these pages keeps state in the URL — the profile and the settings
 * pages are sections, not tabs — so the route declares no search of its own.
 */
function mount(
  path: "/profile" | "/security" | "/apps",
  component: () => React.ReactElement | null,
) {
  const root = createRootRoute({ component: Outlet });
  const route = createRoute({
    getParentRoute: () => root,
    path,
    component,
  });
  const home = createRoute({
    getParentRoute: () => root,
    path: "/",
    component: () => <h1>Console home</h1>,
  });
  const router = createRouter({
    routeTree: root.addChildren([route, home]),
    history,
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return router;
}

describe("profile", () => {
  it("shows every section at once, with no control to work through", async () => {
    history.push("/profile");
    mount("/profile", Profile);

    // Both blocks are on the page as it opens: the display name a reader came
    // to change, and the avatar beside it.
    expect(
      await screen.findByRole("heading", { name: "Display name" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Avatar" })).toBeInTheDocument();
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("keeps the username, which the page reports rather than lets a reader edit", async () => {
    history.push("/profile");
    mount("/profile", Profile);

    expect(await screen.findByText("alice")).toBeInTheDocument();
  });
});

describe("connected applications", () => {
  it("says so plainly when nothing has been approved", async () => {
    history.push("/apps");
    mount("/apps", ConnectedApps);

    expect(
      await screen.findByText("No approved applications yet"),
    ).toBeInTheDocument();
  });

  it("lists an approval with the kind the server reported", async () => {
    queryClient.setQueryData(consentQueryOptions().queryKey, [
      {
        clientId: "gh",
        kind: "saml",
        name: "GitHub Enterprise",
        grantedAt: "2026-01-02T03:04:05Z",
        scopes: ["read"],
      },
    ]);
    history.push("/apps");
    mount("/apps", ConnectedApps);

    expect(await screen.findByText("GitHub Enterprise")).toBeInTheDocument();
    expect(screen.getByText("SAML")).toBeInTheDocument();
    // `kind` is echoed, not derived from the client id.
    expect(screen.getByText("read")).toBeInTheDocument();
  });
});
