import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppAccessRule, GroupPreviewView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupMatchesFrame } from "@/pages/admin/groups/GroupMatchesFrame";
import { apiError } from "@/test/app";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const rule: AppAccessRule = {
  version: 1,
  condition: { fact: "login_method", method: "passkey" },
};

const account = (id: number, matched: boolean): GroupPreviewView => ({
  account: {
    id,
    username: `user${id}`,
    displayName: id % 2 === 0 ? `Person ${id}` : "",
  },
  matched,
});

/** `matching` accounts that match, then `others` that do not. */
function accounts(matching: number, others = 0): GroupPreviewView[] {
  return [
    ...Array.from({ length: matching }, (_, i) => account(i + 1, true)),
    ...Array.from({ length: others }, (_, i) =>
      account(matching + i + 1, false),
    ),
  ];
}

/**
 * The rule-preview endpoint over a directory: every account, paged by an
 * offset cursor, with the whole `matchedCount` on each page.
 */
function directory(
  items: GroupPreviewView[],
  { pageSize = 50, total }: { pageSize?: number; total?: number } = {},
) {
  return async (request: Request) => {
    const { cursor } = (await request.clone().json()) as { cursor?: string };
    const start = Number(cursor ?? 0);
    const end = Math.min(items.length, start + pageSize);
    return Response.json({
      items: items.slice(start, end),
      matchedCount: total ?? items.filter((item) => item.matched).length,
      nextCursor: end < items.length ? String(end) : "",
    });
  };
}

function view(currentRule: AppAccessRule = rule) {
  return (
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupMatchesFrame groupId={7} rule={currentRule} />
      </QueryClientProvider>
    </I18nProvider>
  );
}

async function mount(
  response: (request: Request) => Response | Promise<Response>,
) {
  fetchBoundary.mockImplementation(async (request) => response(request));
  const result = render(view());
  await screen.findByRole("heading", { level: 2, name: "Matches right now" });
  return result;
}

const previews = () =>
  fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.url.endsWith("/groups/rule-preview"));

describe("the matches frame", () => {
  it("asks about the saved rule, from the first page", async () => {
    await mount(directory(accounts(2)));
    await screen.findByText("accounts match");

    expect(previews()).toHaveLength(1);
    expect(await previews()[0]?.clone().json()).toEqual({
      version: 1,
      condition: { fact: "login_method", method: "passkey" },
      limit: 100,
    });
  });

  it("headlines the server's total, not what the first page holds", async () => {
    await mount(directory(accounts(3, 4), { total: 137 }));

    expect(await screen.findByText("137")).toBeVisible();
    expect(screen.getByText("accounts match")).toBeVisible();
    expect(screen.queryByText(/first/i)).toBeNull();
  });

  it("says one account in the singular", async () => {
    await mount(directory(accounts(1, 2)));

    expect(await screen.findByText("account matches")).toBeVisible();
  });

  it("says so when nothing matches, and offers no list", async () => {
    await mount(directory(accounts(0, 3)));

    expect(
      await screen.findByText("No account matches the saved rule."),
    ).toBeVisible();
    expect(screen.getByText("0")).toBeVisible();
    expect(screen.queryByRole("button", { name: "See all" })).toBeNull();
  });

  it("shows up to five matching accounts and how many more the total holds", async () => {
    await mount(directory(accounts(8, 2)));

    const list = await screen.findByRole("list", {
      name: "Some of the matching accounts",
    });
    expect(within(list).getAllByRole("listitem")).toHaveLength(5);
    expect(within(list).getByText("user1")).toBeInTheDocument();
    expect(within(list).getByText("Person 2")).toBeInTheDocument();
    expect(within(list).queryByText("user6")).toBeNull();
    expect(screen.getByText("+3")).toBeVisible();
  });

  it("shows no overflow count when five or fewer match", async () => {
    await mount(directory(accounts(5)));

    await screen.findByRole("list", { name: "Some of the matching accounts" });
    expect(screen.queryByText(/^\+/)).toBeNull();
  });

  it("opens See all on the dialog itself, so the first matches are in view", async () => {
    const user = userEvent.setup();
    await mount(directory(accounts(7, 3)));

    await user.click(await screen.findByRole("button", { name: "See all" }));
    const dialog = await screen.findByRole("dialog");
    await waitFor(() => expect(dialog).toHaveFocus());
  });

  it("lists only the matching accounts under See all", async () => {
    const user = userEvent.setup();
    await mount(directory(accounts(7, 3)));

    await user.click(await screen.findByRole("button", { name: "See all" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Accounts that match",
    });
    const rows = within(dialog).getAllByRole("listitem");
    expect(rows).toHaveLength(7);
    expect(within(rows[1] as HTMLElement).getByText("Person 2")).toBeVisible();
    expect(within(rows[1] as HTMLElement).getByText("@user2")).toBeVisible();
    expect(within(dialog).queryByText("@user8")).toBeNull();
    expect(
      within(dialog).queryByRole("button", { name: "Load more" }),
    ).toBeNull();
  });

  it("loads the next page of accounts on request and says how far it has got", async () => {
    const user = userEvent.setup();
    // Six matches among twelve accounts, three accounts a page. The frame reads
    // three pages for faces, so the dialog opens on five of the six.
    const items = Array.from({ length: 12 }, (_, i) =>
      account(i + 1, i % 2 === 0),
    );
    await mount(directory(items, { pageSize: 3 }));

    await user.click(await screen.findByRole("button", { name: "See all" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getAllByRole("listitem")).toHaveLength(5);
    expect(
      within(dialog).getByText("Showing 5 of 6 matching accounts so far."),
    ).toBeVisible();

    await user.click(within(dialog).getByRole("button", { name: "Load more" }));

    await waitFor(() =>
      expect(within(dialog).getAllByRole("listitem")).toHaveLength(6),
    );
    expect(
      within(dialog).queryByRole("button", { name: "Load more" }),
    ).toBeNull();
    expect(within(dialog).queryByText(/so far/)).toBeNull();
  });

  describe("when the matches are sparse", () => {
    // Ten accounts that do not match, then three that do, three accounts a page.
    const sparse = () => [
      ...accounts(0, 10),
      ...accounts(3).map((m) => ({
        ...m,
        account: { ...m.account, id: m.account.id + 100 },
      })),
    ];

    it("shows the total and See all, and no faces, when its pages hold no match", async () => {
      await mount(directory(sparse(), { pageSize: 3 }));

      expect(await screen.findByText("3")).toBeVisible();
      await waitFor(() => expect(previews()).toHaveLength(3));
      expect(
        screen.queryByRole("list", { name: "Some of the matching accounts" }),
      ).toBeNull();
      expect(screen.queryByText(/^\+/)).toBeNull();
      expect(screen.getByRole("button", { name: "See all" })).toBeVisible();
    });

    it("keeps reading on one press of Load more until a match arrives", async () => {
      const user = userEvent.setup();
      await mount(directory(sparse(), { pageSize: 3 }));
      await waitFor(() => expect(previews()).toHaveLength(3));

      await user.click(await screen.findByRole("button", { name: "See all" }));
      const dialog = await screen.findByRole("dialog");
      expect(within(dialog).queryAllByRole("listitem")).toHaveLength(0);
      expect(
        within(dialog).getByText("Showing 0 of 3 matching accounts so far."),
      ).toBeVisible();

      await user.click(
        within(dialog).getByRole("button", { name: "Load more" }),
      );

      // The fourth page holds one account that does not match and two that do.
      await waitFor(() =>
        expect(within(dialog).getAllByRole("listitem")).toHaveLength(2),
      );
      expect(previews()).toHaveLength(4);
    });

    it("offers no more to load once every match is in, though accounts remain unchecked", async () => {
      const user = userEvent.setup();
      // One match, then twenty accounts that do not match.
      await mount(
        directory(
          [
            account(1, true),
            ...accounts(0, 20).map((m) => ({
              ...m,
              account: { ...m.account, id: m.account.id + 100 },
            })),
          ],
          { pageSize: 3 },
        ),
      );

      await user.click(await screen.findByRole("button", { name: "See all" }));
      const dialog = await screen.findByRole("dialog");

      expect(within(dialog).getAllByRole("listitem")).toHaveLength(1);
      expect(
        within(dialog).queryByRole("button", { name: "Load more" }),
      ).toBeNull();
      expect(within(dialog).queryByText(/so far/)).toBeNull();
    });

    it("counts the faces drawn, not the total less five", async () => {
      // Two faces on the pages read, out of seven matches.
      const items = [...accounts(0, 6), account(101, true), account(102, true)];
      await mount(directory(items, { pageSize: 3, total: 7 }));

      const list = await screen.findByRole("list", {
        name: "Some of the matching accounts",
      });
      expect(within(list).getAllByRole("listitem")).toHaveLength(2);
      expect(screen.getByText("+5")).toBeVisible();
    });
  });

  it("counts again for a different saved rule", async () => {
    let total = 2;
    const { rerender } = await mount((request) =>
      directory(accounts(2), { total })(request),
    );
    expect(await screen.findByText("2")).toBeVisible();

    total = 9;
    rerender(
      view({
        version: 1,
        condition: { fact: "login_method", method: "federation" },
      }),
    );

    expect(await screen.findByText("9")).toBeVisible();
    expect(previews()).toHaveLength(2);
  });

  it("tells a failed read in place, and no toast", async () => {
    await mount(() => apiError("internal_error", 500));

    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "See all" })).toBeNull();
    await waitFor(() => expect(notifyError).not.toHaveBeenCalled());
  });

  it("asks again when Try again is pressed", async () => {
    const user = userEvent.setup();
    let failing = true;
    await mount((request) =>
      failing
        ? apiError("internal_error", 500)
        : directory(accounts(4))(request),
    );
    await user.click(await screen.findByRole("button", { name: "Try again" }));
    failing = false;
    // The first press may have raced the failure; press again if it is still there.
    await waitFor(async () => {
      const retry = screen.queryByRole("button", { name: "Try again" });
      if (retry !== null) await user.click(retry);
      expect(screen.getByText("4")).toBeVisible();
    });
  });

  it("notes that a count may be out of date when a refresh fails", async () => {
    let failing = false;
    await mount((request) =>
      failing
        ? apiError("internal_error", 500)
        : directory(accounts(4))(request),
    );
    expect(await screen.findByText("4")).toBeVisible();

    failing = true;
    await queryClient.refetchQueries({ queryKey: ["admin", "group-matches"] });

    expect(
      await screen.findByText(
        "The latest check failed, so this may be out of date.",
      ),
    ).toBeVisible();
    expect(screen.getByText("4")).toBeVisible();
  });
});
