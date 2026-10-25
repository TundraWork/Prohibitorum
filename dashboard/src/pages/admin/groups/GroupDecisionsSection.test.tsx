import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ManualDecisionView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupDecisionsSection } from "@/pages/admin/groups/GroupDecisionsSection";

const day = 86_400_000;
const account = (id: number, username: string, displayName: string) => ({
  id,
  username,
  displayName,
});
const ana: ManualDecisionView = {
  account: account(7, "ana", "Ana Lima"),
  effect: "allow",
  updatedAt: new Date(Date.now() - 14 * day).toISOString(),
};
const cy: ManualDecisionView = {
  account: account(9, "cy", "Cy Dunn"),
  effect: "deny",
  updatedAt: new Date(Date.now() - 3_600_000).toISOString(),
};

type Page = { items: ManualDecisionView[]; nextCursor: string };

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum/groups/7/decisions";

function requests(method: string, suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter(
      (request) =>
        request.method === method &&
        new URL(request.url).pathname.endsWith(suffix),
    );
}
const writes = (suffix: string) => requests("POST", suffix);
const reads = () => requests("GET", "/decisions");

function mount({
  pages = { "": { items: [ana, cy], nextCursor: "" } },
  extra,
}: {
  /** Pages by the cursor that asks for them; the first has none. */
  pages?: Record<string, Page>;
  extra?: (request: Request) => Response | Promise<Response> | undefined;
} = {}) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = await extra?.(request);
    if (canned) return canned;
    const url = new URL(request.url);
    if (request.method === "GET" && url.pathname === base) {
      const page = pages[url.searchParams.get("cursor") ?? ""];
      return page ? Response.json(page) : new Response(null, { status: 404 });
    }
    if (request.method === "GET" && url.pathname.endsWith("/accounts")) {
      return Response.json({
        items: [
          { ...ana.account, disabled: false },
          { ...account(8, "ben", "Ben Ito"), disabled: false },
          { ...cy.account, disabled: false },
          { ...account(10, "old", "Old Hand"), disabled: true },
        ],
      });
    }
    if (request.method === "POST" && url.pathname === base) {
      const body = (await request.clone().json()) as {
        accountId: number;
        effect: "allow" | "deny";
      };
      return Response.json({
        account: account(body.accountId, "ben", "Ben Ito"),
        effect: body.effect,
        updatedAt: new Date().toISOString(),
      });
    }
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupDecisionsSection groupId={7} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const rows = async () =>
  within(await screen.findByRole("list", { name: "Decisions" })).getAllByRole(
    "listitem",
  );

async function openAdd(user: ReturnType<typeof userEvent.setup>) {
  await user.click(
    await screen.findByRole("button", { name: "Add an account" }),
  );
  return screen.findByRole("dialog", { name: "Add an account" });
}

async function choose(
  user: ReturnType<typeof userEvent.setup>,
  dialog: HTMLElement,
  name: RegExp,
) {
  await user.click(within(dialog).getByRole("combobox"));
  await user.click(await screen.findByRole("option", { name }));
}

async function openRowMenu(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(
    await screen.findByRole("button", { name: `More actions for ${name}` }),
  );
}

describe("the decisions", () => {
  it("say what the group decides and show each account with its effect", async () => {
    mount();

    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "Allowed and denied accounts",
      }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "Allow or deny individual accounts. Allowed accounts can use the applications that select this group. Denied accounts cannot, and a deny overrides any other match. Other accounts are not affected by this group.",
      ),
    ).toBeVisible();
    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Ana Lima");
    expect(first).toHaveTextContent("@ana");
    expect(first).toHaveTextContent("Allow");
    expect(first).toHaveTextContent("Updated 2 weeks ago");
    expect(second).toHaveTextContent("Cy Dunn");
    expect(second).toHaveTextContent("Deny");
    expect(second).toHaveTextContent("Updated 1 hour ago");
  });

  it("name an account without a display name by its username, without repeating it", async () => {
    mount({
      pages: {
        "": {
          items: [{ ...ana, account: account(7, "ana", "") }],
          nextCursor: "",
        },
      },
    });

    const [only] = (await rows()) as [HTMLElement];
    expect(only).toHaveTextContent("ana");
    expect(only).not.toHaveTextContent("@ana");
  });

  it("keep the order the server sends", async () => {
    mount({ pages: { "": { items: [cy, ana], nextCursor: "" } } });

    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Cy Dunn");
    expect(second).toHaveTextContent("Ana Lima");
  });

  it("show an empty state, with the add action only in the header", async () => {
    mount({ pages: { "": { items: [], nextCursor: "" } } });

    expect(
      await screen.findByText("No accounts have been allowed or denied yet."),
    ).toBeVisible();
    expect(
      screen.getAllByRole("button", { name: /Add an account/ }),
    ).toHaveLength(1);
  });

  it("load the next page on request and drop the button after the last", async () => {
    const user = userEvent.setup();
    mount({
      pages: {
        "": { items: [ana], nextCursor: "c1" },
        c1: { items: [cy], nextCursor: "" },
      },
    });

    await rows();
    await user.click(await screen.findByRole("button", { name: "Load more" }));

    await waitFor(async () => expect(await rows()).toHaveLength(2));
    expect(
      reads().some((request) => new URL(request.url).search.includes("c1")),
    ).toBe(true);
    const [first, second] = (await rows()) as [HTMLElement, HTMLElement];
    expect(first).toHaveTextContent("Ana Lima");
    expect(second).toHaveTextContent("Cy Dunn");
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull();
  });

  it("report an unreadable list once, in the section, and read again on request", async () => {
    const user = userEvent.setup();
    let failing = true;
    mount({
      extra: (request) =>
        failing &&
        request.method === "GET" &&
        new URL(request.url).pathname === base
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
    });

    await screen.findByRole("button", { name: "Try again" });
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();

    failing = false;
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Ana Lima")).toBeVisible();
  });
});

describe("adding an account", () => {
  it("allows the chosen account, says so and reads the list again", async () => {
    const user = userEvent.setup();
    mount();
    await rows();
    const before = reads().length;

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ben Ito/);
    await user.click(
      within(dialog).getByRole("button", { name: "Allow Ben Ito" }),
    );

    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 8,
      effect: "allow",
    });
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.group.decision-saved" }),
      ),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Add an account" }),
      ).not.toBeInTheDocument(),
    );
    await waitFor(() => expect(reads().length).toBeGreaterThan(before));
  });

  it("denies the chosen account when Deny is picked", async () => {
    const user = userEvent.setup();
    mount();

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ben Ito/);
    await user.click(within(dialog).getByRole("radio", { name: "Deny" }));
    await user.click(
      within(dialog).getByRole("button", { name: "Deny Ben Ito" }),
    );

    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 8,
      effect: "deny",
    });
  });

  it("asks for an account when none is chosen", async () => {
    const user = userEvent.setup();
    mount();

    const dialog = await openAdd(user);
    await user.click(within(dialog).getByRole("button", { name: "Allow" }));

    expect(
      await within(dialog).findByText("Choose an account first."),
    ).toBeVisible();
    expect(writes("/decisions")).toHaveLength(0);
  });

  it("lists a disabled account with the reason and cannot pick it", async () => {
    const user = userEvent.setup();
    mount();

    const dialog = await openAdd(user);
    await user.click(within(dialog).getByRole("combobox"));

    const option = await screen.findByRole("option", { name: /Old Hand/ });
    expect(option).toHaveTextContent("Disabled account");
    expect(option).toHaveAttribute("aria-disabled", "true");
  });

  it("shows an account's current decision and changes it", async () => {
    const user = userEvent.setup();
    mount();
    await rows();

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ana Lima/);

    // Allow is already the decision, so saving it again would change nothing.
    expect(
      within(dialog).getByText(
        "Ana Lima is already allowed in this group. Choose Deny to change it.",
      ),
    ).toBeVisible();
    const same = within(dialog).getByRole("button", { name: "Allow Ana Lima" });
    expect(same).toHaveAttribute("aria-disabled", "true");
    expect(same).toHaveAttribute("data-refused");
    expect(same).toHaveAccessibleDescription(
      "Ana Lima is already allowed in this group. Choose Deny to change it.",
    );

    await user.click(within(dialog).getByRole("radio", { name: "Deny" }));
    expect(
      within(dialog).getByText(
        "Ana Lima is allowed in this group now. Saving changes it to deny.",
      ),
    ).toBeVisible();
    await user.click(
      within(dialog).getByRole("button", { name: "Deny Ana Lima" }),
    );

    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 7,
      effect: "deny",
    });
  });

  it("keeps the name of an account found beyond the first results", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) => {
        const url = new URL(request.url);
        return request.method === "GET" &&
          url.pathname.endsWith("/accounts") &&
          url.searchParams.get("q") === "late"
          ? Response.json({
              items: [
                {
                  id: 41,
                  username: "late",
                  displayName: "Late Comer",
                  disabled: false,
                },
              ],
            })
          : undefined;
      },
    });

    const dialog = await openAdd(user);
    await user.click(within(dialog).getByRole("combobox"));
    await user.type(
      await screen.findByRole("combobox", { name: "Search" }),
      "late",
    );
    await user.click(await screen.findByRole("option", { name: /Late Comer/ }));

    // The search resets after a pick, so the first results no longer list the
    // account; the field still names it rather than showing its number.
    const field = within(dialog).getByRole("combobox");
    await user.click(field);
    await user.clear(await screen.findByRole("combobox", { name: "Search" }));
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.filter(
          ([request]) =>
            new URL(request.url).search === "" &&
            new URL(request.url).pathname.endsWith("/accounts"),
        ).length,
      ).toBeGreaterThan(1),
    );
    await user.keyboard("{Escape}");
    expect(field).toHaveTextContent("Late Comer");
    expect(field).not.toHaveTextContent("41");
    expect(
      within(dialog).getByRole("button", { name: "Allow Late Comer" }),
    ).toBeVisible();
  });

  it("shows a refused add in the dialog, once, with no success toast", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" && new URL(request.url).pathname === base
          ? Response.json(
              { code: "account_not_found", requestId: "r1" },
              { status: 404 },
            )
          : undefined,
    });

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ben Ito/);
    await user.click(
      within(dialog).getByRole("button", { name: "Allow Ben Ito" }),
    );

    await within(dialog).findByRole("alert");
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
    expect(dialog).toBeVisible();
  });

  it("ignores dismissal while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount({
      extra: (request) =>
        request.method === "POST" && new URL(request.url).pathname === base
          ? new Promise<Response>((resolve) => {
              release = resolve;
            })
          : undefined,
    });

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ben Ito/);
    await user.click(
      within(dialog).getByRole("button", { name: "Allow Ben Ito" }),
    );
    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));

    await user.keyboard("{Escape}");
    expect(
      screen.getByRole("dialog", { name: "Add an account" }),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Cancel" }),
    ).toBeDisabled();

    release(
      Response.json({
        account: account(8, "ben", "Ben Ito"),
        effect: "allow",
        updatedAt: new Date().toISOString(),
      }),
    );
    await waitFor(() =>
      expect(
        screen.queryByRole("dialog", { name: "Add an account" }),
      ).not.toBeInTheDocument(),
    );
  });
});

describe("changing a decision", () => {
  it("switches an allow to a deny without asking, and reads the list again", async () => {
    const user = userEvent.setup();
    mount();
    await rows();
    const before = reads().length;

    await openRowMenu(user, "Ana Lima");
    await user.click(
      await screen.findByRole("menuitem", { name: "Change to deny" }),
    );

    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 7,
      effect: "deny",
    });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.group.decision-saved" }),
      ),
    );
    await waitFor(() => expect(reads().length).toBeGreaterThan(before));
  });

  it("switches a deny to an allow", async () => {
    const user = userEvent.setup();
    mount();

    await openRowMenu(user, "Cy Dunn");
    await user.click(
      await screen.findByRole("menuitem", { name: "Change to allow" }),
    );

    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 9,
      effect: "allow",
    });
  });

  it("reports a refused switch once, in the section", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" && new URL(request.url).pathname === base
          ? Response.json(
              { code: "rate_limited", requestId: "r2" },
              { status: 429 },
            )
          : undefined,
    });

    await openRowMenu(user, "Ana Lima");
    await user.click(
      await screen.findByRole("menuitem", { name: "Change to deny" }),
    );

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });
});

describe("clearing a decision", () => {
  it("clears an allow at once and reads the list again", async () => {
    const user = userEvent.setup();
    mount();
    await rows();
    const before = reads().length;

    await openRowMenu(user, "Ana Lima");
    await user.click(await screen.findByRole("menuitem", { name: "Clear" }));

    await waitFor(() => expect(writes("/decisions/clear")).toHaveLength(1));
    expect(await writes("/decisions/clear")[0]?.json()).toEqual({
      accountId: 7,
    });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    await waitFor(() =>
      expect(notifySuccess).toHaveBeenCalledWith(
        expect.objectContaining({ id: "success.group.decision-cleared" }),
      ),
    );
    await waitFor(() => expect(reads().length).toBeGreaterThan(before));
  });

  it("confirms before clearing a deny, saying what it may let through", async () => {
    const user = userEvent.setup();
    mount();

    await openRowMenu(user, "Cy Dunn");
    await user.click(await screen.findByRole("menuitem", { name: "Clear" }));

    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveAccessibleName("Clear this deny?");
    expect(
      dialog.querySelector('[data-slot="confirm-subject"]'),
    ).toHaveTextContent("Cy Dunn");
    expect(dialog).toHaveTextContent(
      "The account is no longer denied, but may still get access through another group or rule.",
    );
    expect(writes("/decisions/clear")).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Clear" }));
    await waitFor(() => expect(writes("/decisions/clear")).toHaveLength(1));
    expect(await writes("/decisions/clear")[0]?.json()).toEqual({
      accountId: 9,
    });
  });

  it("sends nothing when the clearing of a deny is cancelled", async () => {
    const user = userEvent.setup();
    mount();

    await openRowMenu(user, "Cy Dunn");
    await user.click(await screen.findByRole("menuitem", { name: "Clear" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(writes("/decisions/clear")).toHaveLength(0);
  });

  it("reports a refused clear once, in the section", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/decisions/clear")
          ? Response.json(
              { code: "rate_limited", requestId: "r3" },
              { status: 429 },
            )
          : undefined,
    });

    await openRowMenu(user, "Ana Lima");
    await user.click(await screen.findByRole("menuitem", { name: "Clear" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });
});

describe("a failed clear of a deny", () => {
  it("closes the confirmation and reports the failure once, in the section", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "POST" &&
        new URL(request.url).pathname.endsWith("/decisions/clear")
          ? Response.json(
              { code: "rate_limited", requestId: "r4" },
              { status: 429 },
            )
          : undefined,
    });

    await openRowMenu(user, "Cy Dunn");
    await user.click(await screen.findByRole("menuitem", { name: "Clear" }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Clear" }));

    expect(
      await screen.findByText("Too many requests. Wait a moment."),
    ).toBeVisible();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(notifySuccess).not.toHaveBeenCalled();
  });
});

describe("a write in flight", () => {
  it("holds every row's menu until it settles, so no failure is lost", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount({
      extra: (request) =>
        request.method === "POST" && new URL(request.url).pathname === base
          ? new Promise<Response>((resolve) => {
              release = resolve;
            })
          : undefined,
    });

    await openRowMenu(user, "Ana Lima");
    await user.click(
      await screen.findByRole("menuitem", { name: "Change to deny" }),
    );

    const ana = await screen.findByRole("button", {
      name: "More actions for Ana Lima",
    });
    const cy = screen.getByRole("button", { name: "More actions for Cy Dunn" });
    await waitFor(() => expect(ana).toHaveAttribute("aria-busy", "true"));
    expect(cy).toBeDisabled();
    expect(cy).not.toHaveAttribute("aria-busy");
    // Adding takes over the same write, so it waits as well.
    expect(
      screen.getByRole("button", { name: "Add an account" }),
    ).toBeDisabled();

    release(
      Response.json({
        account: account(7, "ana", "Ana Lima"),
        effect: "deny",
        updatedAt: new Date().toISOString(),
      }),
    );
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "More actions for Cy Dunn" }),
      ).toBeEnabled(),
    );
  });
});

describe("an account whose decision is on a page not yet loaded", () => {
  const pages = {
    "": { items: [ana], nextCursor: "c1" },
    c1: { items: [cy], nextCursor: "" },
  };

  it("reads on before stating anything, then shows the decision found", async () => {
    const user = userEvent.setup();
    let release: () => void = () => {};
    mount({
      pages,
      extra: (request) =>
        request.method === "GET" &&
        new URL(request.url).searchParams.get("cursor") === "c1"
          ? new Promise<Response>((resolve) => {
              release = () => resolve(Response.json(pages.c1));
            })
          : undefined,
    });
    await rows();

    const dialog = await openAdd(user);
    await choose(user, dialog, /Cy Dunn/);

    expect(
      await within(dialog).findByText(
        "Checking this account's current decision…",
      ),
    ).toBeVisible();
    const submit = within(dialog).getByRole("button", {
      name: "Allow Cy Dunn",
    });
    expect(submit).toHaveAttribute("aria-disabled", "true");
    await user.click(submit);
    expect(writes("/decisions")).toHaveLength(0);

    release();
    expect(
      await within(dialog).findByText(
        "Cy Dunn is denied in this group now. Saving changes it to allow.",
      ),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Allow Cy Dunn" }),
    ).not.toHaveAttribute("aria-disabled");
  });

  it("says the decision could not be read, and still allows the save", async () => {
    const user = userEvent.setup();
    mount({
      pages,
      extra: (request) =>
        request.method === "GET" &&
        new URL(request.url).searchParams.get("cursor") === "c1"
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
    });
    await rows();

    const dialog = await openAdd(user);
    await choose(user, dialog, /Cy Dunn/);

    expect(
      await within(dialog).findByText(
        "The current decision for Cy Dunn could not be read. Saving may overwrite an existing one.",
      ),
    ).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(
      within(dialog).getByText(/could not be read\. Saving may overwrite/),
    ).toHaveAttribute("role", "alert");
    expect(notifyError).not.toHaveBeenCalled();
    await user.click(
      within(dialog).getByRole("button", { name: "Allow Cy Dunn" }),
    );
    await waitFor(() => expect(writes("/decisions")).toHaveLength(1));
    expect(await writes("/decisions")[0]?.json()).toEqual({
      accountId: 9,
      effect: "allow",
    });
  });

  it("tries a failed later page once per choice before giving up", async () => {
    const user = userEvent.setup();
    let laterReads = 0;
    mount({
      pages,
      extra: (request) => {
        if (
          request.method === "GET" &&
          new URL(request.url).searchParams.get("cursor") === "c1"
        ) {
          laterReads += 1;
          return laterReads === 1
            ? Response.json({ code: "internal_error" }, { status: 500 })
            : Response.json(pages.c1);
        }
        return undefined;
      },
    });
    await rows();
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    await waitFor(() => expect(laterReads).toBe(1));

    const dialog = await openAdd(user);
    await choose(user, dialog, /Cy Dunn/);

    expect(
      await within(dialog).findByText(
        "Cy Dunn is denied in this group now. Saving changes it to allow.",
      ),
    ).toBeVisible();
    expect(laterReads).toBe(2);
  });

  it("holds the submit while the first page is still on its way", async () => {
    const user = userEvent.setup();
    let release: () => void = () => {};
    mount({
      extra: (request) =>
        request.method === "GET" && new URL(request.url).pathname === base
          ? new Promise<Response>((resolve) => {
              release = () =>
                resolve(Response.json({ items: [cy], nextCursor: "" }));
            })
          : undefined,
    });

    const dialog = await openAdd(user);
    await choose(user, dialog, /Cy Dunn/);

    expect(
      await within(dialog).findByText(
        "Checking this account's current decision…",
      ),
    ).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Allow Cy Dunn" }),
    ).toHaveAttribute("aria-disabled", "true");

    release();
    expect(
      await within(dialog).findByText(
        "Cy Dunn is denied in this group now. Saving changes it to allow.",
      ),
    ).toBeVisible();
  });

  it("says the decision could not be read when the first page failed, and allows the save", async () => {
    const user = userEvent.setup();
    mount({
      extra: (request) =>
        request.method === "GET" && new URL(request.url).pathname === base
          ? Response.json({ code: "internal_error" }, { status: 500 })
          : undefined,
    });
    await screen.findByRole("button", { name: "Try again" });

    const dialog = await openAdd(user);
    await choose(user, dialog, /Cy Dunn/);

    expect(
      await within(dialog).findByText(
        "The current decision for Cy Dunn could not be read. Saving may overwrite an existing one.",
      ),
    ).toBeVisible();
    // The section already reports the unreadable list, so this is a status.
    expect(
      within(dialog).getByText(/could not be read\. Saving may overwrite/),
    ).toHaveAttribute("role", "status");
    expect(within(dialog).queryByRole("alert")).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
    expect(
      within(dialog).getByRole("button", { name: "Allow Cy Dunn" }),
    ).not.toHaveAttribute("aria-disabled");
  });

  it("states nothing for an account with no decision once every page is read", async () => {
    const user = userEvent.setup();
    mount({ pages });
    await rows();

    const dialog = await openAdd(user);
    await choose(user, dialog, /Ben Ito/);

    await waitFor(() =>
      expect(
        within(dialog).getByRole("button", { name: "Allow Ben Ito" }),
      ).not.toHaveAttribute("aria-disabled"),
    );
    expect(
      within(dialog).queryByText(/Checking|could not be read|in this group/),
    ).toBeNull();
  });
});
