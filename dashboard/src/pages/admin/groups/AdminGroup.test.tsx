import type { QueryClient } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView, GroupApplicationView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";
import { groupFixture, ruleGroupFixture } from "@/test/group";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";

function open(group: AppGroupView, used: GroupApplicationView[] = []) {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/groups/${group.id}`]: () => Response.json(group),
    [`GET ${base}/groups/${group.id}/applications`]: () =>
      Response.json({ items: used, nextCursor: "" }),
    [`GET ${base}/groups/providers`]: () => Response.json([]),
    [`POST ${base}/groups/rule-preview`]: () =>
      Response.json({ matchedCount: 0, items: [], nextCursor: "" }),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountPage(group: AppGroupView, used?: GroupApplicationView[]) {
  const api = open(group, used);
  renderApp(`/admin/groups/${group.id}`, queryClient);
  await screen.findByRole("heading", { level: 1, name: group.displayName });
  return api;
}

const header = () =>
  screen.getByRole("heading", { level: 1 }).parentElement as HTMLElement;

describe("the group header", () => {
  it("names a manual group with its kind, identifier and sending status", async () => {
    await mountPage(groupFixture());

    expect(within(header()).getByText("Manual group")).toBeVisible();
    expect(within(header()).getByText("staff")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Copy the identifier" }),
    ).toBeVisible();
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(statuses).toHaveTextContent("Offered to applications");
  });

  it("names a rule group by its kind", async () => {
    await mountPage(ruleGroupFixture({ exposedToDownstream: false }));

    expect(within(header()).getByText("Rule group")).toBeVisible();
    expect(screen.getByRole("list", { name: "Status" })).toHaveTextContent(
      "Not offered",
    );
  });
});

describe("the page's sections", () => {
  it("has details, used-by and deletion for a manual group, and no rule", async () => {
    await mountPage(groupFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Details" }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { level: 2, name: "Used by" }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", { level: 2, name: "Delete" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("heading", { level: 2, name: "Rule" }),
    ).toBeNull();
  });

  it("adds the rule for a rule group", async () => {
    await mountPage(ruleGroupFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Rule" }),
    ).toBeVisible();
    expect(screen.getByText("Who the rule matches")).toBeVisible();
  });

  it("lists the applications that use the group beside it", async () => {
    await mountPage(groupFixture(), [
      { kind: "oidc", appId: "grafana", displayName: "Grafana" },
    ]);

    const side = screen.getByRole("complementary", { name: "Group context" });
    expect(
      await within(side).findByRole("link", { name: /Grafana/ }),
    ).toHaveAttribute("href", "/admin/oidc-applications/grafana");
  });
});

describe("saving the details", () => {
  it("stays on the page and refreshes the header from the saved record", async () => {
    const user = userEvent.setup();
    let stored = groupFixture();
    const api = fakeApi({
      ...publicApi({ ...testSession, role: "admin" }),
      [`GET ${base}/oidc-applications`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/saml-applications`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/forward-auth-apps`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/groups/7`]: () => Response.json(stored),
      [`GET ${base}/groups/7/applications`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`PUT ${base}/groups/7`]: async (request) => {
        stored = { ...stored, ...(await request.json()) };
        return Response.json(stored);
      },
    });
    renderApp("/admin/groups/7", queryClient);
    await screen.findByRole("heading", { level: 1, name: "Staff" });

    const name = screen.getByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Employees");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByRole("heading", { level: 1, name: "Employees" }),
    ).toBeVisible();
    expect(api.sent("PUT", `${base}/groups/7`)).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });
});
