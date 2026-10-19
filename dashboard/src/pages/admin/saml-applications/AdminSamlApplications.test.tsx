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
import { type SamlApp, samlAppFixture } from "@/test/saml-app";

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
    [`GET ${base}/saml-applications`]: list,
    [`GET ${base}/saml-applications/7`]: () => Response.json(samlAppFixture()),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountList(
  items: SamlApp[],
  role: "admin" | "user" = "admin",
): Promise<HTMLElement> {
  serve(role, () => Response.json({ items, nextCursor: "" }));
  renderApp("/admin/saml-applications", queryClient);
  await screen.findByRole("heading", { level: 1, name: "SAML applications" });
  return await screen.findByRole("table", { name: "SAML applications" });
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("the page", () => {
  it("heads the list with what it is for and offers New application to an admin", async () => {
    await mountList([samlAppFixture()]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(
      screen.getByRole("link", { name: "New application" }),
    ).toHaveAttribute("href", "/admin/saml-applications/new");
  });

  it("offers a delegated manager the list without New application", async () => {
    const table = await mountList([samlAppFixture()], "user");

    expect(
      within(table).getByRole("link", { name: "Company wiki" }),
    ).toBeVisible();
    expect(screen.queryByRole("link", { name: "New application" })).toBeNull();
  });

  it("says there are no applications when the list is empty", async () => {
    await mountList([]);
    expect(await screen.findByText("No SAML applications yet")).toBeVisible();
  });
});

describe("a row", () => {
  it("names the application, its Entity ID and its NameID format", async () => {
    const table = await mountList([samlAppFixture()]);

    const row = rowOf(table, "Company wiki");
    expect(
      within(row).getByText("https://wiki.example.test/saml/metadata"),
    ).toBeVisible();
    // Drawn in the theme's muted foreground, which stays legible in dark mode.
    const format = within(row).getByText("persistent");
    expect(format).toBeVisible();
    expect(format).toHaveAttribute("data-slot", "name-id-format");
  });

  it("says when the signing certificate expires, in words", async () => {
    const table = await mountList([samlAppFixture()]);
    expect(
      within(rowOf(table, "Company wiki")).getByText("Expires Mar 15, 2031"),
    ).toBeVisible();
  });

  it("says an expired certificate is expired, not only in colour", async () => {
    const table = await mountList([
      samlAppFixture({
        keys: [{ use: "signing", notAfter: "2020-01-01T12:00:00Z" }],
      }),
    ]);
    expect(
      within(rowOf(table, "Company wiki")).getByText("Expired Jan 1, 2020"),
    ).toBeVisible();
  });

  it("says there is no signing certificate rather than leaving the cell blank", async () => {
    const table = await mountList([samlAppFixture({ keys: [] })]);
    expect(
      within(rowOf(table, "Company wiki")).getByText("No signing certificate"),
    ).toBeVisible();
  });

  it("reads the earliest signing expiry, not an encryption key's", async () => {
    const table = await mountList([
      samlAppFixture({
        keys: [
          { use: "encryption", notAfter: "2020-01-01T12:00:00Z" },
          { use: "signing", notAfter: "2031-03-15T12:00:00Z" },
        ],
      }),
    ]);
    expect(
      within(rowOf(table, "Company wiki")).getByText("Expires Mar 15, 2031"),
    ).toBeVisible();
  });

  it("says in words that a disabled application is disabled", async () => {
    const table = await mountList([
      samlAppFixture({ disabled: true }),
      samlAppFixture({
        id: 8,
        entityId: "https://b.example.test",
        displayName: "Other",
      }),
    ]);

    expect(
      within(rowOf(table, "Company wiki")).getByText("Disabled"),
    ).toBeVisible();
    expect(within(rowOf(table, "Other")).queryByText("Disabled")).toBeNull();
  });

  it("marks a restricted application with a lock that has an accessible name", async () => {
    const table = await mountList([samlAppFixture({ accessRestricted: true })]);
    expect(
      within(rowOf(table, "Company wiki")).getByRole("img", {
        name: "Available only to the selected user groups",
      }),
    ).toBeVisible();
  });

  it("opens the application from the row action", async () => {
    const user = userEvent.setup();
    const table = await mountList([samlAppFixture()]);

    await user.click(
      within(table).getByRole("button", { name: "Open Company wiki" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Company wiki" });
  });
});

describe("a failed read", () => {
  it("is reported once, on the page, and the empty state does not contradict it", async () => {
    serve("admin", () => apiError("internal_error", 500));
    renderApp("/admin/saml-applications", queryClient);

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No SAML applications yet")).toBeNull();
  });
});
