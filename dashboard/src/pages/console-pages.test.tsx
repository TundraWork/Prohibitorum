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
import { render, screen, waitFor } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { consentQueryOptions, sessionQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ConnectedApps } from "@/pages/ConnectedApps";
import { profileTab, securityTab } from "@/pages/console/tabs";
import { Profile } from "@/pages/Profile";
import { Security } from "@/pages/Security";

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
 * Mounts one page under a router that owns the tab search parameter.
 *
 * The route is built the same way the real route file builds it, because the
 * page reads its tab through `Route.useSearch`: a differently declared search
 * would send the page down a different path than the one under test.
 */
function mount(
  path: "/profile" | "/security" | "/apps",
  component: () => React.ReactElement | null,
  validateSearch: (search: Record<string, unknown>) => Record<string, unknown>,
) {
  const root = createRootRoute({ component: Outlet });
  const route = createRoute({
    getParentRoute: () => root,
    path,
    validateSearch,
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

const profileSearch = (search: Record<string, unknown>) => ({
  tab: profileTab(search.tab),
});
const securitySearch = (search: Record<string, unknown>) => ({
  tab: securityTab(search.tab),
});

/** Path plus query, so a tab change is visible in the URL the user would share. */
function location(router: {
  state: { location: { pathname: string; search: unknown } };
}) {
  const { pathname, search } = router.state.location;
  const query = new URLSearchParams(
    Object.entries(search as Record<string, string>),
  ).toString();
  return query ? `${pathname}?${query}` : pathname;
}

describe("profile tabs", () => {
  it("renders the tab the query names and only that panel's content", async () => {
    history.push("/profile?tab=avatar");
    mount("/profile", Profile, profileSearch);

    expect(
      await screen.findByRole("heading", { name: "Avatar" }),
    ).toBeInTheDocument();
    // Only the selected panel is mounted, so its queries never start early.
    expect(
      screen.queryByRole("heading", { name: "Display name" }),
    ).not.toBeInTheDocument();
  });

  it("falls back to the first tab for an unknown value instead of failing", async () => {
    history.push("/profile?tab=nonsense");
    mount("/profile", Profile, profileSearch);

    expect(
      await screen.findByRole("heading", { name: "Display name" }),
    ).toBeInTheDocument();
  });

  it("writes the chosen tab into the URL and replaces rather than stacks history", async () => {
    history.push("/profile");
    const router = mount("/profile", Profile, profileSearch);
    const user: UserEvent = userEvent.setup();
    await screen.findByRole("heading", { name: "Display name" });

    const before = router.history.length;
    await user.click(screen.getByRole("tab", { name: "Avatar" }));

    await waitFor(() => expect(location(router)).toBe("/profile?tab=avatar"));
    // Replaced, not pushed: browsing tabs adds no history entries, so the page
    // the user came from is still one press back.
    expect(router.history.length).toBe(before);
  });
});

describe("security tabs", () => {
  it("mounts only the selected area, and does not request the others", async () => {
    history.push("/security?tab=sessions");
    mount("/security", Security, securitySearch);

    expect(
      await screen.findByRole("tab", { name: "Active sessions" }),
    ).toHaveAttribute("aria-selected", "true");
    // The passkeys panel is not mounted, so its list is never fetched.
    expect(
      fetchBoundary.mock.calls.some(([request]) =>
        new URL(request.url).pathname.endsWith("/me/credentials"),
      ),
    ).toBe(false);
    expect(screen.queryByRole("button", { name: "Add a passkey" })).toBeNull();
  });

  it("keeps the URL and the selected tab in step through a switch", async () => {
    history.push("/security");
    const router = mount("/security", Security, securitySearch);
    const user: UserEvent = userEvent.setup();
    await screen.findByRole("button", { name: "Add a passkey" });

    const before = router.history.length;
    await user.click(screen.getByRole("tab", { name: "Access tokens" }));

    await waitFor(() =>
      expect(
        screen.getByRole("tab", { name: "Access tokens" }),
      ).toHaveAttribute("aria-selected", "true"),
    );
    expect(location(router)).toBe("/security?tab=tokens");
    // A switch replaces, so it adds nothing to the history stack.
    expect(router.history.length).toBe(before);
  });
});

describe("connected applications", () => {
  it("says so plainly when nothing has been approved", async () => {
    history.push("/apps");
    mount("/apps", ConnectedApps, () => ({}));

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
    mount("/apps", ConnectedApps, () => ({}));

    expect(await screen.findByText("GitHub Enterprise")).toBeInTheDocument();
    expect(screen.getByText("SAML")).toBeInTheDocument();
    // `kind` is echoed, not derived from the client id.
    expect(screen.getByText("read")).toBeInTheDocument();
  });
});
