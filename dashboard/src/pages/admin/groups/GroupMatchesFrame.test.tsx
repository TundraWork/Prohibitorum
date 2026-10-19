import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { GroupPreviewView } from "@/api/raw-admin-paths";
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

async function mount(response: () => Response | Promise<Response>) {
  fetchBoundary.mockImplementation(async () => response());
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupMatchesFrame groupId={7} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  await screen.findByRole("heading", { level: 2, name: "Matches right now" });
}

const page = (items: GroupPreviewView[]) => () =>
  Response.json({ items, nextCursor: "" });

describe("the matches frame", () => {
  it("asks for a page of a known size", async () => {
    await mount(page(accounts(2)));
    await screen.findByText("accounts match");

    const request = fetchBoundary.mock.calls[0]?.[0] as Request;
    expect(new URL(request.url).searchParams.get("limit")).toBe("50");
  });

  it("counts the matches among the accounts it checked, and states no total", async () => {
    await mount(page(accounts(3, 4)));

    expect(await screen.findByText("3")).toBeVisible();
    expect(screen.getByText("accounts match")).toBeVisible();
    expect(screen.queryByText(/of 7/)).toBeNull();
    expect(screen.queryByText(/first/i)).toBeNull();
  });

  it("says one account in the singular", async () => {
    await mount(page(accounts(1, 2)));

    expect(await screen.findByText("account matches")).toBeVisible();
  });

  it("says so when nothing matches, and offers no list", async () => {
    await mount(page(accounts(0, 3)));

    expect(
      await screen.findByText("No account matches the saved rule."),
    ).toBeVisible();
    expect(screen.getByText("0")).toBeVisible();
    expect(screen.queryByRole("button", { name: "See all" })).toBeNull();
  });

  it("shows up to five matching accounts and how many more there are", async () => {
    await mount(page(accounts(8, 2)));

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
    await mount(page(accounts(5)));

    await screen.findByRole("list", { name: "Some of the matching accounts" });
    expect(screen.queryByText(/^\+/)).toBeNull();
  });

  it("lists every matching account, and only those, under See all", async () => {
    const user = userEvent.setup();
    await mount(page(accounts(7, 3)));

    await user.click(await screen.findByRole("button", { name: "See all" }));

    const dialog = await screen.findByRole("dialog", {
      name: "Accounts that match",
    });
    const rows = within(dialog).getAllByRole("listitem");
    expect(rows).toHaveLength(7);
    // A name when it has one; the username is always the mono line.
    expect(within(rows[1] as HTMLElement).getByText("Person 2")).toBeVisible();
    expect(within(rows[1] as HTMLElement).getByText("@user2")).toBeVisible();
    expect(within(rows[0] as HTMLElement).getByText("@user1")).toBeVisible();
    expect(within(dialog).queryByText("@user8")).toBeNull();
    expect(within(dialog).queryByText(/Showing/)).toBeNull();
  });

  it("states the cap when the page was full, in the frame and the dialog", async () => {
    const user = userEvent.setup();
    await mount(page(accounts(10, 40)));

    expect(
      await screen.findByText("Counted among the first 50 accounts."),
    ).toBeVisible();
    await user.click(screen.getByRole("button", { name: "See all" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText("Showing matches among the first 50 accounts."),
    ).toBeVisible();
  });

  it("tells a failed read in place, with a retry, and no toast", async () => {
    await mount(() => apiError("internal_error", 500));

    expect(
      await screen.findByText("This section could not be loaded"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
    await waitFor(() => expect(notifyError).not.toHaveBeenCalled());
  });
});
