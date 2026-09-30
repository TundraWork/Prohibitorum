import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import {
  managedApplicationsQueryOptions,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";
import { searchSerialization } from "@/app/search-params";
import { ConsoleLayout } from "@/components/custom/ConsoleLayout";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { i18n } from "@/i18n";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const history = createBrowserHistory();
let queryClient: QueryClient;

type Session = components["schemas"]["SessionView"];

const admin: Session = {
  id: 1,
  username: "alice",
  displayName: "Alice",
  role: "admin",
  avatarSource: "user",
  avatarSourceLabels: {},
  avatarSourceUrls: {},
};

const member: Session = { ...admin, role: "user" };

const config: PublicConfig = {
  instanceName: "Prohibitorum",
  hasCustomIcon: false,
  iconUrl: "",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessage: "",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Prohibitorum", algorithm: "SHA1", digits: 6, period: 30 },
};

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  fetchBoundary.mockRejectedValue(new Error("no server in this test"));
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

/**
 * Mounts the shell at `path` with a page under it, the way the route tree
 * mounts it: the shell is the parent route and the page renders in its outlet.
 */
async function mount(
  path: string,
  session: Session,
  managed?: { oidc: boolean; saml: boolean; forwardAuth: boolean },
) {
  queryClient.setQueryData(sessionQueryOptions().queryKey, session);
  if (managed) {
    queryClient.setQueryData(
      managedApplicationsQueryOptions().queryKey,
      managed,
    );
  }
  const root = createRootRoute({ component: ConsoleLayout });
  const page = (routePath: string) =>
    createRoute({
      getParentRoute: () => root,
      path: routePath,
      component: () => <p>page</p>,
    });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([
      page("/"),
      page("/admin"),
      page("/profile"),
      page("/admin/users"),
      page("/admin/users/$id"),
      page("/admin/saml-applications"),
    ]),
    history,
  });
  history.push(path);
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return screen.findByRole("navigation", { name: "Console navigation" });
}

const names = (list: HTMLElement) =>
  within(list)
    .getAllByRole("link")
    .map((link) => link.textContent);

describe("console navigation", () => {
  it("draws every entry as a link and splits management into three groups", async () => {
    const nav = await mount("/", admin);

    for (const link of within(nav).getAllByRole("link")) {
      expect(link).toHaveAttribute("href");
    }
    expect(
      within(nav).getByRole("link", { name: "Forward-auth applications" }),
    ).toHaveAttribute("href", "/admin/forward-auth-apps");
    expect(names(within(nav).getByRole("list", { name: "Directory" }))).toEqual(
      ["Users", "User groups", "Invitations", "Federation"],
    );
    expect(
      names(within(nav).getByRole("list", { name: "Applications" })),
    ).toEqual([
      "OIDC applications",
      "SAML applications",
      "Forward-auth applications",
    ]);
    expect(names(within(nav).getByRole("list", { name: "System" }))).toEqual([
      "Logs",
      "Settings",
    ]);
  });

  it("keeps a section marked on a record inside it", async () => {
    const nav = await mount("/admin/users/5", admin);

    expect(within(nav).getByRole("link", { name: "Users" })).toHaveAttribute(
      "aria-current",
      "page",
    );
    expect(
      within(nav).getByRole("link", { name: "Console home" }),
    ).not.toHaveAttribute("aria-current");
  });

  it("marks only the console home on the console home", async () => {
    const nav = await mount("/", admin);

    const current = within(nav)
      .getAllByRole("link")
      .filter((link) => link.getAttribute("aria-current") === "page");
    expect(current.map((link) => link.textContent)).toEqual(["Console home"]);
  });

  it("shows a SAML manager the one group their assignment reaches", async () => {
    const nav = await mount("/", member, {
      oidc: false,
      saml: true,
      forwardAuth: false,
    });

    expect(
      names(within(nav).getByRole("list", { name: "Applications" })),
    ).toEqual(["SAML applications"]);
    for (const name of ["Directory", "System"]) {
      expect(within(nav).queryByRole("list", { name })).not.toBeInTheDocument();
    }
  });

  it("shows an unassigned member no management group at all", async () => {
    const nav = await mount("/", member, {
      oidc: false,
      saml: false,
      forwardAuth: false,
    });

    expect(
      within(nav).getByRole("list", { name: "Your account" }),
    ).toBeVisible();
    for (const name of ["Directory", "Applications", "System"]) {
      expect(within(nav).queryByRole("list", { name })).not.toBeInTheDocument();
    }
  });
});

describe("console header", () => {
  it("names an address with no page as not found", async () => {
    await mount("/admin/nope", admin);

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Page not found",
    );
  });

  it("keeps the title of a page that has no section of its own", async () => {
    await mount("/admin", admin);

    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Console home",
    );
  });
});

describe("console sidebar", () => {
  it("names the instance above the navigation", async () => {
    const navigation = await mount("/", member);
    const sidebar = screen.getByRole("complementary");
    const name = within(sidebar).getByText("Prohibitorum");
    expect(
      name.compareDocumentPosition(navigation) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });
});
