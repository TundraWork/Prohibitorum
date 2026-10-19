import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configureSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { type Account, accountFixture } from "@/test/account";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

const base = "/api/prohibitorum";
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
  vi.mocked(HTMLElement.prototype.scrollIntoView).mockClear();
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const page = () => Response.json({ items: [], nextCursor: "" });

function serve(
  account: Account,
  extra: Record<string, () => Response | Promise<Response>> = {},
) {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/accounts/${account.id}`]: () => Response.json(account),
    [`GET ${base}/accounts/${account.id}/identities`]: () => Response.json([]),
    [`GET ${base}/accounts/${account.id}/credentials`]: page,
    [`GET ${base}/accounts/${account.id}/sessions`]: page,
    [`GET ${base}/accounts/${account.id}/tokens`]: page,
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
    ...extra,
  });
}

async function openAccount(account: Account, search = "") {
  const api = serve(account);
  const router = renderApp(`/admin/users/${account.id}${search}`, queryClient);
  await screen.findByRole("heading", { level: 1 });
  return { api, router };
}

describe("the header", () => {
  it("names the account, with its username to copy", async () => {
    const user = userEvent.setup();
    await openAccount(accountFixture());

    expect(
      screen.getByRole("heading", { level: 1, name: "Bob Stone" }),
    ).toBeVisible();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    expect(screen.getByText("@bob")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Copy the username" }));
    // The username is shown with its @, but is copied bare.
    expect(writeText).toHaveBeenCalledWith("bob");
  });

  it("falls back to the username when there is no display name", async () => {
    await openAccount(accountFixture({ displayName: "" }));
    expect(
      screen.getByRole("heading", { level: 1, name: "bob" }),
    ).toBeVisible();
  });

  it("states the role and that the account is enabled", async () => {
    await openAccount(accountFixture());
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["User", "Enabled"]);
  });

  it("states an admin, and a disabled account", async () => {
    await openAccount(accountFixture({ role: "admin", disabled: true }));
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Admin", "Disabled"]);
  });

  it("gives a role it does not know as a fact instead of a status", async () => {
    await openAccount(accountFixture({ role: "member" }));
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled"]);
    expect(screen.getByText("Role member")).toBeVisible();
  });

  it("says when the account joined, and nothing about sign-ins it does not know", async () => {
    await openAccount(accountFixture());
    expect(screen.getByText(/^Joined .*2026/)).toBeVisible();
    expect(screen.queryByText(/signed in/)).toBeNull();
  });

  it("says when it last signed in if the record says so", async () => {
    await openAccount(accountFixture({ lastSignInAt: "2026-02-01T00:00:00Z" }));
    expect(screen.getByText(/^Last signed in/)).toBeVisible();
  });
});

describe("an old ?tab= link", () => {
  it.each(["access", "danger", "profile"])(
    "lands on the %s section as a hash",
    async (tab) => {
      const { router } = await openAccount(accountFixture(), `?tab=${tab}`);
      await waitFor(() => expect(router.state.location.hash).toBe(tab));
      expect(router.state.location.search).toEqual({});
      expect(document.getElementById(tab)).not.toBeNull();
    },
  );

  it("replaces the entry rather than adding one", async () => {
    const { router } = await openAccount(accountFixture(), "?tab=danger");
    await waitFor(() => expect(router.state.location.hash).toBe("danger"));
    expect(router.history.length).toBe(1);
  });

  it("leaves a page without a tab alone", async () => {
    const { router } = await openAccount(accountFixture());
    expect(router.state.location.hash).toBe("");
  });
});

describe("the registration link", () => {
  async function issue(user: ReturnType<typeof userEvent.setup>) {
    await user.click(
      screen.getByRole("button", { name: "Issue a registration link" }),
    );
  }

  it("says an earlier link stops working", async () => {
    await openAccount(accountFixture());
    expect(
      screen.getByRole("heading", { level: 2, name: "Registration link" }),
    ).toBeVisible();
    expect(screen.getByText(/any earlier link stops working/)).toBeVisible();
  });

  it("reveals the link once, in a dialog that stays until it is saved", async () => {
    const user = userEvent.setup();
    const account = accountFixture();
    const api = serve(account, {
      [`POST ${base}/accounts/reissue-enrollment`]: () =>
        Response.json({
          url: "https://id.example.test/enroll/abc",
          expiresAt: "2030-01-01T10:00:00Z",
        }),
    });
    renderApp(`/admin/users/${account.id}`, queryClient);
    await screen.findByRole("heading", { level: 1 });

    await issue(user);

    const dialog = await screen.findByRole("dialog", {
      name: "Copy the registration link",
    });
    expect(within(dialog).getByRole("textbox")).toHaveValue(
      "https://id.example.test/enroll/abc",
    );
    expect(
      api.sent("POST", `${base}/accounts/reissue-enrollment`)[0]?.body,
    ).toEqual({
      id: 2,
    });
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeVisible();
    expect(screen.getByText(/^The link expires on/)).toBeVisible();

    await user.click(
      within(dialog).getByRole("checkbox", {
        name: "I have saved the registration link",
      }),
    );
    await user.click(within(dialog).getByRole("button", { name: "Done" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.getByText(/^The link expires on/)).toBeVisible();
  });

  it("asks for the identity check first when the window is closed", async () => {
    const user = userEvent.setup();
    const account = accountFixture();
    const set = vi.fn();
    const api = serve(account, {
      [`GET ${base}/me/sudo/methods`]: () =>
        Response.json({ methods: ["password_totp"], fresh: false }),
    });
    renderApp(`/admin/users/${account.id}`, queryClient);
    await screen.findByRole("heading", { level: 1 });
    configureSudo({
      queryClient,
      set,
      setFresh: () => {},
      getFresh: () => false,
    });

    await issue(user);

    await waitFor(() => expect(set).toHaveBeenCalled());
    expect(
      api.sent("POST", `${base}/accounts/reissue-enrollment`),
    ).toHaveLength(0);
  });

  it("reports a refusal once, with no toast", async () => {
    const user = userEvent.setup();
    const account = accountFixture();
    serve(account, {
      [`POST ${base}/accounts/reissue-enrollment`]: () =>
        apiError("internal_error", 500),
    });
    renderApp(`/admin/users/${account.id}`, queryClient);
    await screen.findByRole("heading", { level: 1 });

    await issue(user);

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});

describe("the access lists", () => {
  it("are four sections between the profile and the registration link", async () => {
    await openAccount(accountFixture());
    const headings = screen
      .getAllByRole("heading", { level: 2 })
      .map((heading) => heading.textContent);
    expect(headings).toEqual([
      "Profile",
      "Connected identities",
      "Passkeys",
      "Active sessions",
      "Access tokens",
      "Registration link",
      "Disable or delete",
    ]);
  });
});
