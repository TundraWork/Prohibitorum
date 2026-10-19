import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { accountFixture } from "@/test/account";
import {
  apiError,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { providerFixture } from "@/test/identity-provider";

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
const listPath = `${base}/accounts`;

const github = providerFixture({
  slug: "github",
  displayName: "GitHub",
  searchFields: [
    // `eq` is not an operator the server defines, so it is never offered.
    { key: "email", operators: ["exact", "prefix", "contains", "eq"] },
    { key: "subject", operators: ["exact"] },
  ],
});

const alice = accountFixture({
  id: 1,
  username: "alice",
  displayName: "Alice Admin",
  role: "admin",
  lastSignInAt: new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString(),
});
const bob = accountFixture({
  id: 2,
  username: "bob",
  displayName: "Bob Stone",
});
const carol = accountFixture({
  id: 3,
  username: "carol",
  displayName: "Carol Reyes",
  disabled: true,
});

function page(items: unknown[], nextCursor = "") {
  return () => Response.json({ items, nextCursor });
}

function serve(
  list: (request: Request) => Response | Promise<Response>,
): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${listPath}`]: list,
    [`GET ${base}/identity-providers`]: page([github]),
  });
}

async function mount(
  items: unknown[],
  path = "/admin/users",
): Promise<{ api: FakeApi; router: ReturnType<typeof renderApp> }> {
  const api = serve(page(items));
  const router = renderApp(path, queryClient);
  await screen.findByRole("heading", { level: 1, name: "Users" });
  return { api, router };
}

/** The filters the address carries; the empty ones are the defaults. */
function addressOf(router: ReturnType<typeof renderApp>) {
  return Object.fromEntries(
    [...new URLSearchParams(router.state.location.searchStr)].filter(
      ([, value]) => value !== "",
    ),
  );
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

describe("the page", () => {
  it("says what it is for and offers Invite a user, which leads to the invitation form", async () => {
    await mount([alice]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(screen.getByRole("link", { name: "Invite a user" })).toHaveAttribute(
      "href",
      "/admin/invitations/new",
    );
  });
});

describe("a row", () => {
  it("names the account, its @username, and marks an administrator", async () => {
    await mount([alice, bob]);
    const table = await screen.findByRole("table", { name: "Users" });

    const row = rowOf(table, "Alice Admin");
    expect(within(row).getByText("@alice")).toBeVisible();
    expect(within(row).getByText("Admin")).toBeVisible();
    expect(within(rowOf(table, "Bob Stone")).queryByText("Admin")).toBeNull();
  });

  it("says in words that a disabled account is disabled", async () => {
    await mount([bob, carol]);
    const table = await screen.findByRole("table", { name: "Users" });

    expect(
      within(rowOf(table, "Carol Reyes")).getByText("Disabled"),
    ).toBeVisible();
    expect(
      within(rowOf(table, "Bob Stone")).queryByText("Disabled"),
    ).toBeNull();
  });

  it("links to the account page with its id and nothing in the search", async () => {
    await mount([bob]);
    const table = await screen.findByRole("table", { name: "Users" });

    expect(
      within(table).getByRole("link", { name: "Bob Stone" }),
    ).toHaveAttribute("href", "/admin/users/2");
  });

  it("falls back to the username when the display name is empty", async () => {
    await mount([accountFixture({ id: 4, username: "dave", displayName: "" })]);
    const table = await screen.findByRole("table", { name: "Users" });

    expect(within(table).getByRole("link", { name: "dave" })).toBeVisible();
    expect(within(table).getByText("@dave")).toBeVisible();
  });

  it("reads the last sign-in as a distance, and Never when there is none", async () => {
    await mount([alice, bob]);
    const table = await screen.findByRole("table", { name: "Users" });

    expect(
      within(rowOf(table, "Alice Admin")).getByText("3 days ago"),
    ).toBeVisible();
    expect(within(rowOf(table, "Bob Stone")).getByText("Never")).toBeVisible();
  });

  it("opens the account from the row action", async () => {
    const user = userEvent.setup();
    const { router } = await mount([bob]);
    const table = await screen.findByRole("table", { name: "Users" });

    await user.click(
      within(table).getByRole("button", { name: "Open Bob Stone" }),
    );

    await waitFor(() =>
      expect(router.state.location.pathname).toBe("/admin/users/2"),
    );
  });
});

describe("the filters in the address", () => {
  it("searches the directory as the reader types and keeps the search in the URL", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([alice]);

    await user.type(
      await screen.findByRole("searchbox", { name: "Search users" }),
      "ali",
    );

    await waitFor(() => expect(addressOf(router)).toEqual({ q: "ali" }));
    await waitFor(() =>
      expect(api.sent("GET", listPath).at(-1)?.url.searchParams.get("q")).toBe(
        "ali",
      ),
    );
  });

  it("opens with the search and the identity filter the address carries", async () => {
    const { api } = await mount(
      [alice],
      "/admin/users?q=ali&provider=github&field=email&match=prefix&value=%40corp",
    );

    expect(screen.getByRole("searchbox", { name: "Search users" })).toHaveValue(
      "ali",
    );
    const params = api.sent("GET", listPath)[0]?.url.searchParams;
    expect(Object.fromEntries(params ?? [])).toEqual({
      q: "ali",
      provider: "github",
      field: "email",
      match: "prefix",
      value: "@corp",
    });
  });

  it("does not count the search on the Filters button", async () => {
    await mount([alice], "/admin/users?q=ali");

    expect(
      screen.getByRole("button", { name: /^Filters/ }),
    ).toHaveAccessibleName("Filters");
  });

  it("counts the identity filter on the Filters button", async () => {
    await mount([alice], "/admin/users?provider=github");

    expect(
      screen.getByRole("button", { name: /^Filters/ }),
    ).toHaveAccessibleName("Filters, 1 active");
  });
});

describe("the identity filter", () => {
  async function openFilter(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: /^Filters/ }));
    return await screen.findByRole("dialog", { name: "Filters" });
  }

  async function choose(
    user: ReturnType<typeof userEvent.setup>,
    dialog: HTMLElement,
    field: string,
    option: string,
  ) {
    await user.click(
      await within(dialog).findByRole("combobox", { name: field }),
    );
    await user.click(await screen.findByRole("option", { name: option }));
  }

  it("builds a filter from provider, field, match and value, sending nothing until it is whole", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([alice]);
    const dialog = await openFilter(user);

    await choose(user, dialog, "Provider", "GitHub");
    expect(
      within(dialog).getByText(
        "Fill in the provider, field, match and value to apply this filter.",
      ),
    ).toBeVisible();
    // The server rejects a partial filter, so the list is asked without it.
    expect(
      api
        .sent("GET", listPath)
        .every((request) => !request.url.searchParams.has("provider")),
    ).toBe(true);

    await choose(user, dialog, "Field", "email");
    await choose(user, dialog, "Match", "Starts with");
    await user.type(within(dialog).getByLabelText("Value to match"), "ali");

    await waitFor(() =>
      expect(addressOf(router)).toEqual({
        provider: "github",
        field: "email",
        match: "prefix",
        value: "ali",
      }),
    );
    await waitFor(() => {
      const params = api.sent("GET", listPath).at(-1)?.url.searchParams;
      expect(Object.fromEntries(params ?? [])).toEqual({
        provider: "github",
        field: "email",
        match: "prefix",
        value: "ali",
      });
    });
    expect(
      within(dialog).queryByText(
        "Fill in the provider, field, match and value to apply this filter.",
      ),
    ).toBeNull();
  });

  it("offers only the operators the server defines", async () => {
    const user = userEvent.setup();
    await mount([alice], "/admin/users?provider=github&field=email");
    const dialog = await openFilter(user);

    await user.click(
      await within(dialog).findByRole("combobox", { name: "Match" }),
    );

    expect(await screen.findByRole("option", { name: "Exact" })).toBeVisible();
    expect(screen.getByRole("option", { name: "Starts with" })).toBeVisible();
    expect(screen.getByRole("option", { name: "Contains" })).toBeVisible();
    expect(screen.queryByRole("option", { name: "eq" })).toBeNull();
  });

  it("resets the field, match and value when the provider changes", async () => {
    const user = userEvent.setup();
    const { router } = await mount(
      [alice],
      "/admin/users?provider=github&field=email&match=exact&value=x",
    );
    const dialog = await openFilter(user);

    await choose(user, dialog, "Provider", "Any provider");

    await waitFor(() => expect(addressOf(router)).toEqual({}));
  });

  it("keeps the field and match controls closed until what they depend on is chosen", async () => {
    const user = userEvent.setup();
    await mount([alice]);
    const dialog = await openFilter(user);

    expect(
      within(dialog).getByRole("combobox", { name: "Field" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("combobox", { name: "Match" }),
    ).toBeDisabled();
  });
});

describe("clearing the filters", () => {
  it("is offered only while a filter is set, and drops the search and the identity filter together", async () => {
    const user = userEvent.setup();
    const { router } = await mount(
      [alice],
      "/admin/users?q=ali&provider=github&field=email&match=exact&value=x",
    );

    await user.click(screen.getByRole("button", { name: "Clear filters" }));

    await waitFor(() => expect(addressOf(router)).toEqual({}));
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Clear filters" }),
      ).toBeNull(),
    );
    expect(screen.getByRole("searchbox", { name: "Search users" })).toHaveValue(
      "",
    );
  });

  it("is not on the page while nothing is filtered", async () => {
    await mount([alice]);

    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });
});

describe("an empty list", () => {
  it("says there are no accounts yet when nothing is filtered", async () => {
    await mount([]);

    expect(await screen.findByText("No accounts yet")).toBeVisible();
    expect(screen.queryByText("No account matches these filters")).toBeNull();
  });

  it("says nothing matches when a filter is set, and offers to clear it once in the empty state", async () => {
    const user = userEvent.setup();
    const { router } = await mount([], "/admin/users?q=zzz");

    expect(
      await screen.findByText("No account matches these filters"),
    ).toBeVisible();
    expect(screen.queryByText("No accounts yet")).toBeNull();

    const table = screen.getByRole("table", { name: "Users" });
    await user.click(
      within(table).getByRole("button", { name: "Clear filters" }),
    );

    await waitFor(() => expect(addressOf(router)).toEqual({}));
  });

  it("does not call an unfinished identity filter the reason nothing is listed", async () => {
    await mount([], "/admin/users?provider=github");

    expect(await screen.findByText("No accounts yet")).toBeVisible();
  });
});

describe("a failed read", () => {
  it("is reported once, on the page, with the filters still in reach and no empty state", async () => {
    serve(() => apiError("internal_error", 500));
    renderApp("/admin/users", queryClient);

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No accounts yet")).toBeNull();
    expect(
      screen.getByRole("searchbox", { name: "Search users" }),
    ).toBeVisible();
  });
});

describe("paging", () => {
  const original = globalThis.IntersectionObserver;
  afterEach(() => {
    globalThis.IntersectionObserver = original;
  });

  it("asks for the next page with the cursor the server issued, under the same filters", async () => {
    class InView {
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
      InView as unknown as typeof IntersectionObserver;
    const api = serve((request) =>
      new URL(request.url).searchParams.get("cursor") === "next"
        ? Response.json({ items: [bob], nextCursor: "" })
        : Response.json({ items: [alice], nextCursor: "next" }),
    );

    renderApp("/admin/users?q=a", queryClient);

    const table = await screen.findByRole("table", { name: "Users" });
    await within(table).findByRole("link", { name: "Bob Stone" });
    const second = api.sent("GET", listPath)[1]?.url.searchParams;
    expect(second?.get("cursor")).toBe("next");
    expect(second?.get("q")).toBe("a");
  });
});
