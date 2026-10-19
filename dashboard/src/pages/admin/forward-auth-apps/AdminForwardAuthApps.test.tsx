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
import {
  type ForwardAuthApp,
  forwardAuthAppFixture,
} from "@/test/forward-auth-app";

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

function serve(
  role: "admin" | "user",
  list: () => Response | Promise<Response>,
) {
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/forward-auth-apps`]: list,
    [`GET ${base}/forward-auth-apps/grafana`]: () =>
      Response.json(forwardAuthAppFixture()),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountList(
  items: ForwardAuthApp[],
  role: "admin" | "user" = "admin",
): Promise<HTMLElement> {
  serve(role, () => Response.json({ items, nextCursor: "" }));
  renderApp("/admin/forward-auth-apps", queryClient);
  await screen.findByRole("heading", {
    level: 1,
    name: "Forward-auth applications",
  });
  return await screen.findByRole("table", {
    name: "Forward-auth applications",
  });
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("the page", () => {
  it("heads the list with what it is for and offers New application to an admin", async () => {
    await mountList([forwardAuthAppFixture()]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(
      screen.getByRole("link", { name: "New application" }),
    ).toHaveAttribute("href", "/admin/forward-auth-apps/new");
  });

  it("offers a delegated manager the list without New application", async () => {
    const table = await mountList([forwardAuthAppFixture()], "user");

    expect(within(table).getByRole("link", { name: "Grafana" })).toBeVisible();
    expect(screen.queryByRole("link", { name: "New application" })).toBeNull();
  });

  it("says there are no applications when the list is empty", async () => {
    await mountList([]);
    expect(
      await screen.findByText("No forward-auth applications yet"),
    ).toBeVisible();
  });
});

describe("a row", () => {
  it("names the application, its Client ID, its hostname and its Remote-User", async () => {
    const table = await mountList([forwardAuthAppFixture()]);

    const row = rowOf(table, "Grafana");
    expect(within(row).getByText("grafana")).toBeVisible();
    expect(within(row).getByText("grafana.example.test")).toBeVisible();
    expect(within(row).getByText("Username")).toBeVisible();
  });

  it("shows a dash rather than a blank for a record with no hostname", async () => {
    const table = await mountList([
      forwardAuthAppFixture({ forwardAuthHost: "" }),
    ]);
    expect(within(rowOf(table, "Grafana")).getByText("—")).toBeVisible();
  });

  it("says in words that a disabled application is disabled", async () => {
    const table = await mountList([
      forwardAuthAppFixture({ disabled: true }),
      forwardAuthAppFixture({ clientId: "wiki", displayName: "Wiki" }),
    ]);

    expect(within(rowOf(table, "Grafana")).getByText("Disabled")).toBeVisible();
    expect(within(rowOf(table, "Wiki")).queryByText("Disabled")).toBeNull();
  });

  it("marks a restricted application with a lock that has an accessible name", async () => {
    const table = await mountList([
      forwardAuthAppFixture({ accessRestricted: true }),
    ]);
    expect(
      within(rowOf(table, "Grafana")).getByRole("img", {
        name: "Available only to the selected user groups",
      }),
    ).toBeVisible();
  });

  it("opens the application from the row action", async () => {
    const user = userEvent.setup();
    const table = await mountList([forwardAuthAppFixture()]);

    await user.click(
      within(table).getByRole("button", { name: "Open Grafana" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Grafana" });
  });
});

describe("a failed read", () => {
  it("is reported once, on the page, and the empty state does not contradict it", async () => {
    serve("admin", () => apiError("internal_error", 500));
    renderApp("/admin/forward-auth-apps", queryClient);

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No forward-auth applications yet")).toBeNull();
  });
});
