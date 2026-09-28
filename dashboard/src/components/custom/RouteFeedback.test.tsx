import { I18nProvider } from "@lingui/react";
import {
  type QueryClient,
  QueryClientProvider,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  notFound,
  Outlet,
  RouterProvider,
  rootRouteId,
} from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import type { RequestExchange } from "@/api/exchange";
import { createQueryClient } from "@/app/query-client";
import {
  AppRouteError,
  AppRouteNotFound,
  ConsoleRouteNotFound,
  PublicRouteNotFound,
  RouteError,
  RouteLayoutContext,
} from "@/components/custom/RouteFeedback";
import { i18n } from "@/i18n";

const history = createBrowserHistory();
let queryClient: QueryClient;

/** What each route does when it loads; a test replaces the ones it fails. */
type Behaviour = {
  root?: () => unknown;
  admin?: () => unknown;
  home?: () => unknown;
  login?: () => unknown;
  loginOther?: () => unknown;
  consoleLayout?: () => void;
  adminPage?: () => React.ReactElement;
};
let behaviour: Behaviour;

beforeEach(() => {
  i18n.activate("en");
  behaviour = {};
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.restoreAllMocks();
});

const exchange: RequestExchange = {
  method: "GET",
  path: "/api/prohibitorum/admin/accounts/42",
  response: { status: 503, headers: [["content-type", "application/json"]] },
};

function http(status: number, code: string, extra: Partial<ApiError> = {}) {
  return new ApiError({ kind: "http", status, code, ...extra });
}

/**
 * The app's route shape without its layouts: a root, a console layout and a
 * public layout, each declaring the views the real route files declare.
 */
function mount(path: string) {
  const root = createRootRoute({
    loader: () => behaviour.root?.(),
    errorComponent: AppRouteError,
    component: Outlet,
  });
  const consoleLayout = createRoute({
    getParentRoute: () => root,
    id: "_console",
    errorComponent: AppRouteError,
    notFoundComponent: ConsoleRouteNotFound,
    component: function ConsoleLayout() {
      behaviour.consoleLayout?.();
      return (
        <RouteLayoutContext value="console">
          <Outlet />
        </RouteLayoutContext>
      );
    },
  });
  const publicLayout = createRoute({
    getParentRoute: () => root,
    id: "_public",
    errorComponent: AppRouteError,
    notFoundComponent: PublicRouteNotFound,
    component: () => (
      <RouteLayoutContext value="public">
        <Outlet />
      </RouteLayoutContext>
    ),
  });
  const home = createRoute({
    getParentRoute: () => consoleLayout,
    path: "/",
    loader: () => behaviour.home?.(),
    component: () => <p>Console home page</p>,
  });
  const admin = createRoute({
    getParentRoute: () => consoleLayout,
    path: "/admin",
    loader: () => behaviour.admin?.(),
    component: () => behaviour.adminPage?.() ?? <p>Admin page</p>,
  });
  const login = createRoute({
    getParentRoute: () => publicLayout,
    path: "/login",
    loader: () => behaviour.login?.(),
    component: () => <p>Sign-in page</p>,
  });
  const loginOther = createRoute({
    getParentRoute: () => publicLayout,
    path: "/login/other",
    loader: () => behaviour.loginOther?.(),
    component: () => <p>Other public page</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([
      consoleLayout.addChildren([home, admin]),
      publicLayout.addChildren([login, loginOther]),
    ]),
    history,
    defaultErrorComponent: RouteError,
    defaultNotFoundComponent: AppRouteNotFound,
  });
  history.push(path);
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return router;
}

/** A value to hand back later, for a load that should still be running. */
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((settle) => {
    resolve = settle;
  });
  return { promise, resolve };
}

const spinnerIn = (element: HTMLElement) =>
  element.querySelector('[data-slot="spinner"]');

describe("console pages", () => {
  it("draws a failed page under the header with its facts and a retry", async () => {
    const user = userEvent.setup();
    const second = deferred();
    let calls = 0;
    behaviour.admin = () => {
      calls += 1;
      if (calls === 1) {
        throw http(503, "server_error", { requestId: "req-abc123", exchange });
      }
      return second.promise;
    };
    mount("/admin");

    const title = await screen.findByRole("alert");
    expect(title).toHaveTextContent(
      "The server could not complete the request.",
    );
    expect(screen.queryByRole("heading", { level: 1 })).not.toBeInTheDocument();

    const facts = document.querySelector("dl");
    if (!facts) throw new Error("no facts");
    const terms = [...facts.querySelectorAll("dt")].map((dt) => dt.textContent);
    const values = [...facts.querySelectorAll("dd")].map(
      (dd) => dd.textContent,
    );
    expect(terms).toEqual(["Request", "Status", "Error code", "Request ID"]);
    expect(values).toEqual([
      "GET /api/prohibitorum/admin/accounts/42",
      "503",
      "server_error",
      "req-abc123",
    ]);

    const retry = screen.getByRole("button", { name: "Try again" });
    await user.click(retry);
    await waitFor(() => expect(spinnerIn(retry)).not.toBeNull());
    second.resolve();
    expect(await screen.findByText("Admin page")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("opens the request details from the failed page", async () => {
    const user = userEvent.setup();
    behaviour.admin = () => {
      throw http(503, "server_error", { requestId: "req-abc123", exchange });
    };
    mount("/admin");

    await user.click(await screen.findByRole("button", { name: "Details" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("GET /api/prohibitorum/admin/accounts/42"),
    ).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
  });

  it("fetches a failed suspense query again on retry", async () => {
    const user = userEvent.setup();
    let calls = 0;
    const queryFn = async () => {
      calls += 1;
      if (calls === 1) throw http(500, "server_error");
      return "loaded";
    };
    behaviour.adminPage = function AdminPage() {
      const { data } = useSuspenseQuery({ queryKey: ["admin"], queryFn });
      return <p>Admin data {data}</p>;
    };
    mount("/admin");

    await user.click(await screen.findByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Admin data loaded")).toBeInTheDocument();
    expect(calls).toBe(2);
  });

  it("offers the way home when the server refuses the page", async () => {
    const user = userEvent.setup();
    behaviour.admin = () => {
      throw http(403, "not_admin");
    };
    const router = mount("/admin");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This requires an administrator.",
    );
    expect(
      screen.queryByRole("button", { name: "Try again" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Back to console home" }),
    );
    expect(await screen.findByText("Console home page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("offers no way home on the home page itself", async () => {
    behaviour.home = () => {
      throw http(404, "account_not_found");
    };
    mount("/");

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "That account no longer exists.",
    );
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});

describe("public pages", () => {
  it("replaces the page's title and offers the way back to sign-in", async () => {
    const user = userEvent.setup();
    behaviour.loginOther = () => {
      throw http(400, "bad_request");
    };
    const router = mount("/login/other");

    const heading = await screen.findByRole("heading", {
      level: 1,
      name: "Unable to load this page",
    });
    expect(heading).toHaveFocus();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(
      screen.getByText("The request could not be accepted."),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Back to sign-in" }));
    expect(await screen.findByText("Sign-in page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("offers no way back on the sign-in page itself", async () => {
    behaviour.login = () => {
      throw http(400, "bad_request");
    };
    mount("/login");

    await screen.findByRole("heading", { name: "Unable to load this page" });
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});

describe("recovery", () => {
  it("signs in again when the session is gone", async () => {
    const user = userEvent.setup();
    behaviour.admin = () => {
      throw http(401, "no_session");
    };
    const router = mount("/admin");

    await user.click(await screen.findByRole("button", { name: "Sign in" }));
    expect(await screen.findByText("Sign-in page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("does not offer to sign in when only a step-up is missing", async () => {
    behaviour.admin = () => {
      throw http(401, "sudo_required");
    };
    mount("/admin");

    await screen.findByRole("alert");
    expect(
      screen.queryByRole("button", { name: "Sign in" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Back to console home" }),
    ).toBeInTheDocument();
  });

  it("signs a disabled account out", async () => {
    const user = userEvent.setup();
    const fetch = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(null, { status: 204 }));
    behaviour.admin = () => {
      throw http(403, "account_disabled");
    };
    const router = mount("/admin");

    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    expect(await screen.findByText("Sign-in page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/login");
    const request = fetch.mock.calls[0]?.[0];
    expect(request).toBeInstanceOf(Request);
    expect((request as Request).method).toBe("POST");
    expect(new URL((request as Request).url).pathname).toBe(
      "/api/prohibitorum/auth/logout",
    );
  });
});

describe("whole-window failures", () => {
  it("draws a broken layout on its own card with a reload", async () => {
    const user = userEvent.setup();
    vi.spyOn(console, "error").mockImplementation(() => undefined);
    const reload = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({
      ...window.location,
      reload,
    });
    behaviour.consoleLayout = () => {
      throw new Error("boom");
    };
    mount("/admin");

    expect(
      await screen.findByRole("heading", {
        level: 1,
        name: "Unable to load this page",
      }),
    ).toHaveFocus();
    expect(
      screen.getByText("Something went wrong on this page."),
    ).toBeInTheDocument();
    expect(screen.getByText("Error: boom")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Details" }),
    ).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Reload" }));
    expect(reload).toHaveBeenCalledOnce();
  });

  it("offers a retry when the root route cannot reach the server", async () => {
    behaviour.root = () => {
      throw new ApiError({ kind: "network" });
    };
    mount("/admin");

    expect(
      await screen.findByRole("heading", { name: "Unable to load this page" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Could not connect to the server. Check your connection.",
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Try again" }),
    ).toBeInTheDocument();
  });
});

describe("addresses with no page", () => {
  it("draws an address no layout claims on its own card", async () => {
    const user = userEvent.setup();
    const router = mount("/does-not-exist");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toHaveFocus();
    await user.click(screen.getByRole("button", { name: "Go home" }));
    expect(await screen.findByText("Console home page")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("draws an address under the console inside the console", async () => {
    mount("/admin/nope");

    expect(
      await screen.findByRole("button", { name: "Back to console home" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("draws an address under sign-in on the sign-in card", async () => {
    mount("/login/nope");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Back to sign-in" }),
    ).toBeInTheDocument();
  });

  it("draws a page that hides itself as the root's not-found view", async () => {
    behaviour.admin = () => {
      throw notFound({ routeId: rootRouteId });
    };
    mount("/admin");

    expect(
      await screen.findByRole("heading", { level: 1, name: "Page not found" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Go home" })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Back to console home" }),
    ).not.toBeInTheDocument();
  });
});
