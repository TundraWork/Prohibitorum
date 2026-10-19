import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
  AppGroupView,
  GroupApplicationView,
  ManualDecisionView,
} from "@/api/raw-admin-paths";
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

let queryClient: QueryClient;
const notifyError = vi.fn<(error: unknown) => void>();

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

function open(
  group: AppGroupView,
  used: GroupApplicationView[] = [],
  decisions: ManualDecisionView[] = [],
) {
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
    [`GET ${base}/groups/${group.id}/decisions`]: () =>
      Response.json({ items: decisions, nextCursor: "" }),
    [`GET ${base}/groups/providers`]: () => Response.json([]),
    [`POST ${base}/groups/rule-preview`]: () =>
      Response.json({ matchedCount: 0, items: [], nextCursor: "" }),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountPage(
  group: AppGroupView,
  used?: GroupApplicationView[],
  decisions?: ManualDecisionView[],
) {
  const api = open(group, used, decisions);
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
    expect(statuses).toHaveTextContent("Sent to applications");
  });

  it("names a rule group by its kind", async () => {
    await mountPage(ruleGroupFixture({ exposedToDownstream: false }));

    expect(within(header()).getByText("Rule group")).toBeVisible();
    expect(screen.getByRole("list", { name: "Status" })).toHaveTextContent(
      "Not sent to applications",
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
      screen.queryByRole("heading", {
        level: 2,
        name: "Who this group matches",
      }),
    ).toBeNull();
  });

  it("adds the rule for a rule group", async () => {
    await mountPage(ruleGroupFixture());

    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "Who this group matches",
      }),
    ).toBeVisible();
    expect(screen.getByRole("tab", { name: "Builder" })).toBeVisible();
  });

  it("puts who a rule group matches, and the account check, above where it is used", async () => {
    await mountPage(ruleGroupFixture());

    const side = screen.getByRole("complementary", { name: "Group context" });
    const titles = within(side)
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent);
    expect(titles).toEqual([
      "Matches right now",
      "Check an account",
      "Used by",
    ]);
  });

  it("leaves both out of a manual group's side column", async () => {
    await mountPage(groupFixture());

    const side = screen.getByRole("complementary", { name: "Group context" });
    expect(
      within(side).queryByRole("heading", { name: "Matches right now" }),
    ).toBeNull();
    expect(
      within(side).queryByRole("heading", { name: "Check an account" }),
    ).toBeNull();
  });

  it("puts who a manual group decides under its details, with the counts beside it above where it is used", async () => {
    await mountPage(
      groupFixture(),
      [],
      [
        {
          account: { id: 1, username: "ana", displayName: "Ana Lima" },
          effect: "allow",
          updatedAt: new Date().toISOString(),
        },
        {
          account: { id: 2, username: "cy", displayName: "Cy Dunn" },
          effect: "deny",
          updatedAt: new Date().toISOString(),
        },
      ],
    );

    const main = screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent);
    expect(main.slice(0, 2)).toEqual(["Details", "Who this group decides"]);
    const side = screen.getByRole("complementary", { name: "Group context" });
    expect(await within(side).findByText("1 allowed · 1 denied")).toBeVisible();
    expect(
      within(side)
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Decisions", "Used by"]);
  });

  it("leaves the decisions out of a rule group", async () => {
    await mountPage(ruleGroupFixture());

    expect(
      screen.queryByRole("heading", { name: "Who this group decides" }),
    ).toBeNull();
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
      [`GET ${base}/groups/7/decisions`]: () =>
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

describe("deleting the group", () => {
  it("leaves for the list without an error toast for the record it just removed", async () => {
    const user = userEvent.setup();
    let deleted = false;
    const api = fakeApi({
      ...publicApi({ ...testSession, role: "admin" }),
      [`GET ${base}/oidc-applications`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/saml-applications`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/forward-auth-apps`]: () =>
        Response.json({ items: [], nextCursor: "" }),
      // Once deleted, every read of the group is a 404, as on the server.
      [`GET ${base}/groups/7`]: () =>
        deleted
          ? apiError("group_not_found", 404)
          : Response.json(groupFixture()),
      [`GET ${base}/groups/7/applications`]: () =>
        deleted
          ? apiError("group_not_found", 404)
          : Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/groups/7/decisions`]: () =>
        deleted
          ? apiError("group_not_found", 404)
          : Response.json({ items: [], nextCursor: "" }),
      [`GET ${base}/groups`]: () =>
        Response.json(deleted ? [] : [groupFixture()]),
      [`POST ${base}/groups/7/delete`]: () => {
        deleted = true;
        return new Response(null, { status: 204 });
      },
    });
    const router = renderApp("/admin/groups/7", queryClient);
    await screen.findByRole("heading", { level: 1, name: "Staff" });

    const remove = screen.getByRole("button", { name: "Delete" });
    await waitFor(() => expect(remove).toBeEnabled());
    await user.click(remove);
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Delete group" }),
    );

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/groups"),
    );
    expect(api.sent("POST", `${base}/groups/7/delete`)).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("saving the rule", () => {
  /** A rule group with the side column's reads, and a rule save that changes the count. */
  function openRuleGroup() {
    let stored = ruleGroupFixture();
    let matched = 2;
    const empty = () => Response.json({ items: [], nextCursor: "" });
    const api = fakeApi({
      ...publicApi({ ...testSession, role: "admin" }),
      [`GET ${base}/oidc-applications`]: empty,
      [`GET ${base}/saml-applications`]: empty,
      [`GET ${base}/forward-auth-apps`]: empty,
      [`GET ${base}/groups/8`]: () => Response.json(stored),
      [`GET ${base}/groups/8/applications`]: empty,
      [`GET ${base}/groups/providers`]: () => Response.json([]),
      [`GET ${base}/accounts`]: () =>
        Response.json({
          items: [
            {
              id: 4,
              username: "ana",
              displayName: "Ana Lima",
              disabled: false,
            },
          ],
        }),
      [`GET ${base}/groups/8/explain/4`]: () =>
        Response.json({
          account: { id: 4, username: "ana", displayName: "Ana Lima" },
          explanation: {
            path: "$",
            label: "login_method=passkey",
            result: matched > 2,
          },
        }),
      [`POST ${base}/groups/rule-preview`]: () =>
        Response.json({ matchedCount: matched, items: [], nextCursor: "" }),
      [`PUT ${base}/groups/8`]: async (request) => {
        stored = { ...stored, ...(await request.json()) };
        matched = 3;
        return Response.json(stored);
      },
    });
    return api;
  }

  async function saveAnEditedRule(user: ReturnType<typeof userEvent.setup>) {
    await user.click(
      screen.getAllByRole("button", {
        name: "Add condition",
      })[0] as HTMLElement,
    );
    await user.click(await screen.findByRole("button", { name: "Save rule" }));
  }

  it("counts the matches again for the rule it saved", async () => {
    const user = userEvent.setup();
    openRuleGroup();
    renderApp("/admin/groups/8", queryClient);
    const side = await screen.findByRole("complementary", {
      name: "Group context",
    });
    expect(await within(side).findByText("2")).toBeVisible();

    await saveAnEditedRule(user);

    expect(await within(side).findByText("3")).toBeVisible();
  });

  it("reads the checked account's explanation again for the rule it saved", async () => {
    const user = userEvent.setup();
    const api = openRuleGroup();
    renderApp("/admin/groups/8", queryClient);
    const side = await screen.findByRole("complementary", {
      name: "Group context",
    });
    await user.click(await within(side).findByRole("combobox"));
    await user.click(await screen.findByRole("option", { name: /Ana Lima/ }));
    expect(await within(side).findByText("Not in this group")).toBeVisible();

    await saveAnEditedRule(user);

    expect(await within(side).findByText("In this group")).toBeVisible();
    expect(
      api.sent("GET", `${base}/groups/8/explain/4`).length,
    ).toBeGreaterThan(1);
  });
});
