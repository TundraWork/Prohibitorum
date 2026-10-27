import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
import {
  ConsoleLayout,
  ConsoleNavigation,
  visibleManagementSections,
} from "@/components/custom/ConsoleLayout";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import { SidebarProvider } from "@/components/ui/sidebar";
import { i18n } from "@/i18n";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const history = createBrowserHistory();
let queryClient: QueryClient;

type Session = components["schemas"]["SessionView"];

const admin: Session = {
  id: 1,
  username: "alice",
  oidcSubject: "00000000-0000-4000-8000-000000000001",
  displayName: "Alice",
  role: "admin",
  preferences: { appearance: { scheme: null, mode: null } },
  avatarSource: "user",
};

const member: Session = { ...admin, role: "user" };

const config: PublicConfig = {
  instanceName: "Prohibitorum",
  hasCustomIcon: false,
  iconUrl: "",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessages: {},
  maintenanceDefaultLocale: "en",
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
  vi.restoreAllMocks();
});

type Managed = { oidc: boolean; saml: boolean; forwardAuth: boolean };

/**
 * Mounts the shell at `path` with a page under it, the way the route tree
 * mounts it: the shell is the parent route and the page renders in its outlet.
 */
function renderShell(
  path: string,
  session: Session,
  options: {
    managed?: Managed;
    page?: () => React.ReactElement;
    onSearch?: () => void;
  } = {},
) {
  queryClient.setQueryData(sessionQueryOptions().queryKey, session);
  if (options.managed) {
    queryClient.setQueryData(
      managedApplicationsQueryOptions().queryKey,
      options.managed,
    );
  }
  const Page = options.page ?? (() => <p>page</p>);
  const root = createRootRoute({
    component: () => <ConsoleLayout onSearch={options.onSearch} />,
  });
  const page = (routePath: string) =>
    createRoute({
      getParentRoute: () => root,
      path: routePath,
      component: Page,
    });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([
      page("/"),
      page("/admin"),
      page("/profile"),
      page("/security"),
      page("/admin/users"),
      page("/admin/users/$id"),
      page("/admin/saml-applications"),
      page("/admin/settings"),
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
}

async function mount(
  path: string,
  session: Session,
  managed?: Managed,
  options: Parameters<typeof renderShell>[2] = {},
) {
  renderShell(path, session, { ...options, managed });
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
      ["Users", "User groups", "Invitations", "Identity providers"],
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
    expect(within(nav).getByRole("link", { name: "Home" })).not.toHaveAttribute(
      "aria-current",
    );
  });

  it("leads Home out to the launcher, never marked as the page you are on", async () => {
    const nav = await mount("/profile", admin);

    expect(within(nav).getByRole("link", { name: "Home" })).toHaveAttribute(
      "href",
      "/",
    );
    const current = within(nav)
      .getAllByRole("link")
      .filter((link) => link.getAttribute("aria-current") === "page");
    expect(current.map((link) => link.textContent)).toEqual(["Profile"]);
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

    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByText("Page not found")).toBeVisible();
  });
});

describe("console sidebar", () => {
  it("names the instance above the navigation", async () => {
    const navigation = await mount("/", member);
    const sidebar = document.querySelector('[data-slot="sidebar"]');
    if (!(sidebar instanceof HTMLElement)) throw new Error("no sidebar");
    const name = within(sidebar).getByText("Prohibitorum");
    expect(
      name.compareDocumentPosition(navigation) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("shows a count beside an entry as a badge", async () => {
    const root = createRootRoute({
      component: () => (
        <SidebarProvider>
          <ConsoleNavigation
            managementSections={visibleManagementSections(true, undefined)}
            counts={{ "/admin/users": 42 }}
          />
        </SidebarProvider>
      ),
    });
    const router = createRouter({
      ...searchSerialization,
      routeTree: root.addChildren([]),
      history,
    });
    history.push("/");
    render(
      <I18nProvider i18n={i18n}>
        <RouterProvider router={router} />
      </I18nProvider>,
    );

    const nav = await screen.findByRole("navigation", {
      name: "Console navigation",
    });
    const badge = within(nav).getByText("42");
    expect(badge).toHaveAttribute("data-slot", "sidebar-menu-badge");
    expect(badge.closest("li")).toContainElement(
      within(nav).getByRole("link", { name: "Users" }),
    );
    expect(within(nav).queryByText("0")).not.toBeInTheDocument();
  });
});

describe("console account menu", () => {
  it("offers the account's pages and signing out", async () => {
    const user = userEvent.setup();
    await mount("/", admin);

    await user.click(screen.getByRole("button", { name: /Alice/ }));

    expect(
      await screen.findByRole("menuitem", { name: "Profile" }),
    ).toHaveAttribute("href", "/profile");
    expect(screen.getByRole("menuitem", { name: "Security" })).toHaveAttribute(
      "href",
      "/security",
    );
    expect(
      screen.getByRole("menuitem", { name: "Sign out" }),
    ).toBeInTheDocument();
  });

  it("shows signing out on the menu's trigger and keeps the menu shut meanwhile", async () => {
    fetchBoundary.mockImplementation(() => new Promise<Response>(() => {}));
    const user = userEvent.setup();
    await mount("/", admin);

    const trigger = screen.getByRole("button", { name: /Alice/ });
    await user.click(trigger);
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));

    await waitFor(() => expect(trigger).toHaveAttribute("aria-busy", "true"));
    expect(trigger).toHaveAttribute("aria-disabled", "true");
    expect(document.querySelectorAll('[data-slot="spinner"]')).toHaveLength(1);
    expect(trigger).toContainElement(
      document.querySelector('[data-slot="spinner"]') as HTMLElement,
    );
    await user.click(trigger);
    expect(
      screen.queryByRole("menuitem", { name: "Sign out" }),
    ).not.toBeInTheDocument();
  });

  it("names the account and its role in the footer", async () => {
    await mount("/", member);

    const trigger = screen.getByRole("button", { name: /Alice/ });
    expect(trigger).toHaveTextContent("User");
  });
});

describe("console header breadcrumb", () => {
  it("ends on the section when a page names nothing more", async () => {
    await mount("/admin/users", admin);

    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(crumbs).getByText("Users")).toHaveAttribute(
      "aria-current",
      "page",
    );
    // The page's own header carries the h1; the trail's last item is plain text.
    expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();
  });

  it("ends on the record a page names, under a link back to its section", async () => {
    function Record() {
      useConsoleCrumb("Mei Tanaka");
      return <p>page</p>;
    }
    await mount("/admin/users/5", admin, undefined, { page: Record });

    const crumbs = screen.getByRole("navigation", { name: "Breadcrumb" });
    await waitFor(() =>
      expect(within(crumbs).getByText("Mei Tanaka")).toHaveAttribute(
        "aria-current",
        "page",
      ),
    );
    expect(within(crumbs).getByRole("link", { name: "Users" })).toHaveAttribute(
      "href",
      "/admin/users",
    );
    expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();
  });

  it("draws the search field only when a search is wired", async () => {
    await mount("/", admin);
    expect(screen.queryByRole("searchbox")).not.toBeInTheDocument();
  });

  it("draws the search field with its shortcut when a search is wired", async () => {
    await mount("/", admin, undefined, { onSearch: () => undefined });
    expect(screen.getByRole("searchbox", { name: "Search" })).toBeVisible();
    expect(screen.getByText("⌘K")).toBeVisible();
  });
});

describe("console shell on a narrow screen", () => {
  beforeEach(() => {
    vi.spyOn(window, "matchMedia").mockImplementation(
      (query: string) =>
        ({
          matches: query === "(max-width: 799px)",
          media: query,
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
        }) as unknown as MediaQueryList,
    );
  });

  it("opens the navigation as a sheet from the header trigger", async () => {
    const user = userEvent.setup();
    renderShell("/", member);

    expect(
      screen.queryByRole("navigation", { name: "Console navigation" }),
    ).not.toBeInTheDocument();
    await user.click(
      await screen.findByRole("button", { name: "Toggle sidebar" }),
    );

    const sheet = await screen.findByRole("dialog", {
      name: "Console navigation",
    });
    expect(
      within(sheet).getByRole("navigation", { name: "Console navigation" }),
    ).toBeVisible();
  });

  it("names the sheet in the active language", async () => {
    i18n.activate("zh");
    const user = userEvent.setup();
    renderShell("/", member);

    await user.click(
      await screen.findByRole("button", { name: /Toggle sidebar|侧边栏/ }),
    );

    expect(
      await screen.findByRole("dialog", { name: "控制台导航" }),
    ).toBeVisible();
  });
});

describe("the maintenance banner", () => {
  const banner = () =>
    screen.queryByRole("status", {
      name: "Maintenance mode",
    });

  it("is not drawn while maintenance mode is off", async () => {
    await mount("/admin/users", admin);

    expect(banner()).toBeNull();
  });

  it("is drawn above the page on any console page while it is on, linking to the setting", async () => {
    queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
      ...config,
      maintenanceMode: true,
    });
    await mount("/admin/users", admin);

    const notice = await screen.findByRole("status", {
      name: "Maintenance mode",
    });
    expect(notice).toHaveTextContent(
      "Maintenance mode is on — only administrators can sign in.",
    );
    expect(
      within(notice).getByRole("link", { name: "Maintenance settings" }),
    ).toHaveAttribute("href", "/admin/settings#maintenance");
    expect(screen.getByText("page")).toBeVisible();
  });

  it("follows the saved setting when it is turned off", async () => {
    queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
      ...config,
      maintenanceMode: true,
    });
    await mount("/", admin);
    await screen.findByRole("status", { name: "Maintenance mode" });

    queryClient.setQueryData(publicConfigQueryOptions().queryKey, config);

    await waitFor(() => expect(banner()).toBeNull());
  });

  it("appears once a confirmed save on the maintenance form has refreshed the config", async () => {
    const user = userEvent.setup();
    let saved = config;
    fetchBoundary.mockImplementation(async (request) => {
      if (request.method === "PUT") {
        saved = { ...saved, ...(await request.json()) };
        return new Response(null, { status: 204 });
      }
      return Response.json(saved);
    });
    await mount("/admin/settings", admin, undefined, {
      page: MaintenancePanel,
    });
    await screen.findByRole("switch", { name: "Maintenance mode" });
    expect(banner()).toBeNull();

    await user.click(screen.getByRole("switch", { name: "Maintenance mode" }));
    await user.click(
      screen.getByRole("button", { name: "Turn on maintenance and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Turn on" }));

    expect(
      await screen.findByRole("status", { name: "Maintenance mode" }),
    ).toHaveTextContent(
      "Maintenance mode is on — only administrators can sign in.",
    );
  });
});
