import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { groupFixture, ruleGroupFixture } from "@/test/group";

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

function serve(list: () => Response | Promise<Response>) {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/groups`]: list,
    [`GET ${base}/groups/7`]: () => Response.json(groupFixture()),
    [`GET ${base}/groups/7/applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/groups/7/decisions`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountList(groups: AppGroupView[]): Promise<HTMLElement> {
  serve(() => Response.json(groups));
  renderApp("/admin/groups", queryClient);
  await screen.findByRole("heading", { level: 1, name: "User groups" });
  return await screen.findByRole("table", { name: "User groups" });
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("the page", () => {
  it("heads the list with what groups are for and offers New group", async () => {
    await mountList([groupFixture()]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(screen.getByRole("link", { name: "New group" })).toHaveAttribute(
      "href",
      "/admin/groups/new",
    );
  });

  it("says there are no groups, with one help line and the create action only in the header", async () => {
    await mountList([]);

    expect(await screen.findByText("No user groups yet")).toBeVisible();
    expect(
      screen.getByText(/Create one to choose accounts by hand/),
    ).toBeVisible();
    expect(screen.getAllByRole("link", { name: "New group" })).toHaveLength(1);
  });
});

describe("a row", () => {
  it("names the group with its identifier, kind, description and application count", async () => {
    const table = await mountList([
      groupFixture({ applicationCount: 4 }),
      ruleGroupFixture({ applicationCount: undefined }),
    ]);

    expect(
      within(table)
        .getAllByRole("columnheader")
        .map((header) => header.textContent),
    ).toEqual(["Group", "Kind", "Description", "Applications", ""]);

    const manual = rowOf(table, "Staff");
    expect(within(manual).getByText("staff")).toBeVisible();
    expect(within(manual).getByText("Manual")).toBeVisible();
    expect(within(manual).getByText("Everyone employed here")).toBeVisible();
    expect(within(manual).getByText("4")).toBeVisible();

    const rule = rowOf(table, "Passkey users");
    expect(within(rule).getByText("passkey-users")).toBeVisible();
    expect(within(rule).getByText("Rule")).toBeVisible();
    // No description and no count are both absent, not zero.
    expect(within(rule).getAllByText("—")).toHaveLength(2);
  });

  it("marks a group that is not sent to applications, and only that one", async () => {
    const table = await mountList([
      groupFixture(),
      ruleGroupFixture({ exposedToDownstream: false }),
    ]);

    expect(
      within(rowOf(table, "Passkey users")).getByText(
        "Not sent to applications",
      ),
    ).toBeVisible();
    expect(
      within(rowOf(table, "Staff")).queryByText("Not sent to applications"),
    ).toBeNull();
  });

  it("opens the group from its row action", async () => {
    const user = userEvent.setup();
    const table = await mountList([groupFixture()]);

    await user.click(within(table).getByRole("button", { name: "Open Staff" }));

    await screen.findByRole("heading", { level: 1, name: "Staff" });
  });
});

describe("a list that cannot be read", () => {
  it("says so once on the page, with no table under it and no toast", async () => {
    serve(() => apiError("internal_error", 500));
    renderApp("/admin/groups", queryClient);

    await screen.findByRole("alert");
    await waitFor(() => expect(screen.queryByRole("table")).toBeNull());
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
