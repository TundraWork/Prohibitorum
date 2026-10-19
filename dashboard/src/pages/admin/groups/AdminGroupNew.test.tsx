import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  type FakeApi,
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

/** The rule preview waits this long after an edit before it asks. */
const previewDebounceMs = 400;

const base = "/api/prohibitorum";
const createPath = `${base}/groups`;

function serve(
  create: () => Response | Promise<Response> = () =>
    Response.json(groupFixture(), { status: 201 }),
  created: AppGroupView = groupFixture(),
): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`POST ${createPath}`]: create,
    [`GET ${createPath}`]: () => Response.json([created]),
    [`GET ${createPath}/${created.id}`]: () => Response.json(created),
    [`GET ${createPath}/${created.id}/applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${createPath}/${created.id}/decisions`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${createPath}/providers`]: () => Response.json([]),
    [`POST ${createPath}/rule-preview`]: () =>
      Response.json({ matchedCount: 0, items: [], nextCursor: "" }),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
  });
}

async function mountForm() {
  const user = userEvent.setup();
  const router = renderApp("/admin/groups/new", queryClient);
  await screen.findByRole("heading", { level: 1, name: "New user group" });
  return { user, router };
}

async function fill(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("Name"), "Staff");
  await user.type(screen.getByLabelText("Identifier"), "staff");
}

const createButton = () => screen.getByRole("button", { name: "Create group" });

describe("the form's shape", () => {
  it("offers the two kinds as described choices, manual chosen, and says the kind is final", async () => {
    serve();
    await mountForm();

    const kinds = screen.getByRole("radiogroup", { name: "Kind" });
    const manual = within(kinds).getByRole("radio", { name: "Manual" });
    expect(manual).toBeChecked();
    expect(manual).toHaveAccessibleDescription(/allow or deny by hand/);
    expect(
      within(kinds).getByRole("radio", { name: "Rule" }),
    ).not.toBeChecked();
    expect(
      within(kinds).getByRole("radio", { name: "Rule" }),
    ).toHaveAccessibleDescription(/keeps itself up to date/);
    expect(screen.getByText(/cannot be changed/)).toBeVisible();
  });

  it("names its sections, and for a manual group says where the accounts are chosen", async () => {
    serve();
    await mountForm();

    expect(
      screen
        .getAllByRole("heading", { level: 2 })
        .map((heading) => heading.textContent),
    ).toEqual(["Kind", "Details"]);
    expect(
      screen.getByText(
        "You choose who this group allows or denies on its page after you create it.",
      ),
    ).toBeVisible();
    expect(
      screen.getByRole("switch", { name: "Send to applications" }),
    ).toBeChecked();
  });

  it("adds the rule builder, and says matches wait for the save, for a rule group", async () => {
    serve();
    const { user } = await mountForm();

    await user.click(screen.getByRole("radio", { name: "Rule" }));

    expect(
      screen.getByRole("heading", { level: 2, name: "Who this group matches" }),
    ).toBeVisible();
    expect(
      screen.getByText("Matching accounts appear once the group is saved."),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Add condition" })).toBeVisible();
    expect(
      screen.queryByText(/You choose who this group allows or denies/),
    ).toBeNull();
  });
});

describe("creating a group", () => {
  it("sends a manual group with no rule and opens its page", async () => {
    const api = serve();
    const { user, router } = await mountForm();

    await fill(user);
    await user.type(screen.getByLabelText("Description"), "Everyone here");
    await user.click(createButton());

    await screen.findByRole("heading", { level: 1, name: "Staff" });
    expect(api.sent("POST", createPath)[0]?.body).toEqual({
      kind: "manual",
      slug: "staff",
      displayName: "Staff",
      description: "Everyone here",
      exposedToDownstream: true,
    });
    expect(router.state.location.pathname).toBe("/admin/groups/7");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("sends a rule group with the rule it was built with", async () => {
    const created = ruleGroupFixture();
    const api = serve(() => Response.json(created, { status: 201 }), created);
    const { user, router } = await mountForm();

    await fill(user);
    await user.click(screen.getByRole("radio", { name: "Rule" }));
    await user.click(
      screen.getByRole("switch", { name: "Send to applications" }),
    );
    // A new group has no preview: nothing is saved to count matches against.
    await new Promise((resolve) =>
      setTimeout(resolve, previewDebounceMs + 100),
    );
    expect(api.sent("POST", `${createPath}/rule-preview`)).toHaveLength(0);
    await user.click(createButton());

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/groups/8"),
    );
    expect(api.sent("POST", createPath)[0]?.body).toEqual({
      kind: "rule",
      slug: "staff",
      displayName: "Staff",
      description: "",
      exposedToDownstream: false,
      rule: {
        version: 1,
        condition: {
          op: "all",
          children: [{ fact: "login_method", method: "passkey" }],
        },
      },
    });
  });

  it("refuses an empty name and identifier on the fields, sending nothing", async () => {
    const api = serve();
    const { user } = await mountForm();

    await user.click(createButton());

    expect(await screen.findByText("Give the group a name.")).toBeVisible();
    expect(screen.getByText("Give the group an identifier.")).toBeVisible();
    expect(api.sent("POST", createPath)).toHaveLength(0);
  });

  it("puts a taken identifier on its field, and says it once", async () => {
    serve(() => apiError("group_slug_conflict", 409));
    const { user, router } = await mountForm();

    await fill(user);
    await user.click(createButton());

    const field = screen.getByLabelText("Identifier");
    await waitFor(() => expect(field).toHaveAttribute("aria-invalid", "true"));
    expect(field).toHaveAccessibleDescription(/already/i);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(router.state.location.pathname).toBe("/admin/groups/new");
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("says another failure on the form, and says it once", async () => {
    serve(() => apiError("internal_error", 500));
    const { user } = await mountForm();

    await fill(user);
    await user.click(createButton());

    await screen.findByRole("alert");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("a rule that cannot be saved", () => {
  it("holds Create back with the reason beside it, and sends nothing", async () => {
    const api = serve();
    const { user } = await mountForm();

    await fill(user);
    await user.click(screen.getByRole("radio", { name: "Rule" }));
    await user.click(screen.getByRole("button", { name: "Add condition" }));
    const added = screen.getAllByRole("group", { name: /Signs in with/ })[1];
    await user.click(
      within(added as HTMLElement).getByRole("combobox", { name: "Condition" }),
    );
    await user.click(
      await screen.findByRole("option", { name: "Connected to" }),
    );

    expect(createButton()).toBeDisabled();
    expect(createButton()).toHaveAccessibleDescription(
      "Choose a provider for the highlighted condition.",
    );
    expect(
      screen.getAllByText("Choose a provider for the highlighted condition.")
        .length,
    ).toBeGreaterThan(0);
    expect(api.sent("POST", createPath)).toHaveLength(0);
  });
});

describe("the list afterwards", () => {
  it("is invalidated by a creation, so it shows the new group", async () => {
    serve();
    const { user } = await mountForm();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await fill(user);
    await user.click(createButton());
    await screen.findByRole("heading", { level: 1, name: "Staff" });

    expect(invalidate).toHaveBeenCalledWith(
      expect.objectContaining({ queryKey: ["admin", "groups"] }),
    );
  });
});

describe("leaving", () => {
  it("holds Cancel while the group is being created", async () => {
    let finish: (response: Response) => void = () => {};
    serve(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    );
    const { user } = await mountForm();

    await fill(user);
    await user.click(createButton());

    const cancel = await screen.findByRole("button", { name: "Cancel" });
    expect(cancel).toBeDisabled();
    expect(screen.queryByRole("link", { name: "Cancel" })).toBeNull();
    finish(Response.json(groupFixture(), { status: 201 }));
    await screen.findByRole("heading", { level: 1, name: "Staff" });
  });

  it("returns to the list from Cancel without sending anything", async () => {
    const api = serve();
    const { user, router } = await mountForm();

    await user.click(screen.getByRole("link", { name: "Cancel" }));

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/groups"),
    );
    expect(api.sent("POST", createPath)).toHaveLength(0);
  });
});
