import type { QueryClient } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { type OidcApp, oidcAppFixture } from "@/test/oidc-app";

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";

function page(items: OidcApp[], nextCursor = "") {
  return () => Response.json({ items, nextCursor });
}

function serve(
  role: "admin" | "user",
  list: () => Response | Promise<Response>,
) {
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: list,
    [`GET ${base}/oidc-applications/grafana`]: () =>
      Response.json(oidcAppFixture()),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountList(
  items: OidcApp[],
  role: "admin" | "user" = "admin",
): Promise<HTMLElement> {
  serve(role, page(items));
  renderApp("/admin/oidc-applications", queryClient);
  await screen.findByRole("heading", { level: 1, name: "OIDC applications" });
  return await screen.findByRole("table", { name: "OIDC applications" });
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("the page", () => {
  it("heads the list with what it is for and offers New application to an admin", async () => {
    await mountList([oidcAppFixture()]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(
      screen.getByRole("link", { name: "New application" }),
    ).toHaveAttribute("href", "/admin/oidc-applications/new");
  });

  it("offers a delegated manager the list without New application", async () => {
    const table = await mountList([oidcAppFixture()], "user");

    expect(within(table).getByRole("link", { name: "Grafana" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "New application" })).toBeNull();
    expect(
      screen.queryByRole("button", { name: "New application" }),
    ).toBeNull();
  });

  it("says there are no applications when the list is empty", async () => {
    await mountList([]);
    expect(await screen.findByText("No OIDC applications yet")).toBeVisible();
    expect(screen.getByText(/Register a client to let people/)).toBeVisible();
    // One way to the create page: the header's.
    expect(
      screen.getAllByRole("link", { name: "New application" }),
    ).toHaveLength(1);
  });
});

describe("a row", () => {
  it("names the application, its Client ID, its type and where it redirects", async () => {
    const table = await mountList([
      oidcAppFixture({
        redirectUris: [
          "https://grafana.example.test/cb",
          "https://staging.example.test/cb",
        ],
      }),
    ]);

    const row = rowOf(table, "Grafana");
    expect(within(row).getByText("grafana")).toBeVisible();
    expect(within(row).getByText("Confidential")).toBeVisible();
    expect(within(row).getByText("grafana.example.test")).toBeVisible();
    expect(within(row).getByText("+1")).toBeVisible();
  });

  it("calls a client without a secret public", async () => {
    const table = await mountList([
      oidcAppFixture({ clientAuthMethod: "none" }),
    ]);
    expect(within(rowOf(table, "Grafana")).getByText("Public")).toBeVisible();
  });

  it("says in words that a disabled application is disabled, not only by dimming it", async () => {
    const table = await mountList([
      oidcAppFixture({ disabled: true }),
      oidcAppFixture({ clientId: "wiki", displayName: "Wiki" }),
    ]);

    expect(within(rowOf(table, "Grafana")).getByText("Disabled")).toBeVisible();
    expect(within(rowOf(table, "Wiki")).queryByText("Disabled")).toBeNull();
  });

  it("marks a restricted application with a lock that has an accessible name", async () => {
    const table = await mountList([oidcAppFixture({ accessRestricted: true })]);
    expect(
      within(rowOf(table, "Grafana")).getByRole("img", {
        name: "Available only to the selected user groups",
      }),
    ).toBeVisible();
  });

  it("flags an application with no redirect address as a warning", async () => {
    const table = await mountList([oidcAppFixture({ redirectUris: [] })]);

    const none = within(rowOf(table, "Grafana")).getByText("None");
    expect(none.closest("[data-tone]")).toHaveAttribute("data-tone", "warning");
  });

  it("opens the application from the row action", async () => {
    const user = userEvent.setup();
    const table = await mountList([oidcAppFixture()]);

    await user.click(
      within(table).getByRole("button", { name: "Open Grafana" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Grafana" });
  });
});

describe("paging", () => {
  const original = globalThis.IntersectionObserver;
  afterEach(() => {
    globalThis.IntersectionObserver = original;
  });

  it("asks for the next cursor when the end of the list scrolls into view, keeping the rows in hand", async () => {
    // Reports the sentinel as already on screen, as a short page does.
    class OnScreen {
      constructor(private readonly callback: IntersectionObserverCallback) {}
      observe() {
        this.callback(
          [{ isIntersecting: true } as IntersectionObserverEntry],
          this as unknown as IntersectionObserver,
        );
      }
      disconnect() {}
      unobserve() {}
      takeRecords() {
        return [];
      }
    }
    globalThis.IntersectionObserver =
      OnScreen as unknown as typeof IntersectionObserver;
    const second = oidcAppFixture({ clientId: "wiki", displayName: "Wiki" });
    const api = serve(
      "admin",
      (() => {
        let call = 0;
        return () => {
          call += 1;
          return call === 1
            ? Response.json({
                items: [oidcAppFixture()],
                nextCursor: "after-grafana",
              })
            : Response.json({ items: [second], nextCursor: "" });
        };
      })(),
    );
    renderApp("/admin/oidc-applications", queryClient);

    const table = await screen.findByRole("table", {
      name: "OIDC applications",
    });
    await within(table).findByRole("link", { name: "Wiki" });
    expect(within(table).getByRole("link", { name: "Grafana" })).toBeVisible();
    expect(
      api
        .sent("GET", `${base}/oidc-applications`)
        .some(
          (request) =>
            request.url.searchParams.get("cursor") === "after-grafana",
        ),
    ).toBe(true);
  });
});

describe("a failed read", () => {
  it("is reported once, on the page, and the empty state does not contradict it", async () => {
    serve("admin", () => apiError("internal_error", 500));
    renderApp("/admin/oidc-applications", queryClient);

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No OIDC applications yet")).toBeNull();
  });
});
