import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
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

type AuditEvent = components["schemas"]["AuditEventView"];

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";
const listPath = `${base}/audit-events`;
const accountsPath = `${base}/accounts`;

const bob = accountFixture({
  id: 2,
  username: "bob",
  displayName: "Bob Stone",
});
const carol = accountFixture({
  id: 3,
  username: "carol",
  displayName: "Carol Reyes",
});

function event(id: number, overrides: Partial<AuditEvent> = {}): AuditEvent {
  return {
    id,
    at: "2026-09-25T10:15:30Z",
    factor: "webauthn",
    event: "use",
    ...overrides,
  };
}

/** A passkey use by bob: user agent, address and a detail all to open. */
const passkeyUse = event(5, {
  accountId: 2,
  accountUsername: "bob",
  ip: "198.51.100.7",
  userAgent: "Mozilla/5.0 TestBrowser/1.0",
  detail: { kid: "key-1" },
});
/** A failed password by carol, with a detail only. */
const passwordFailed = event(4, {
  accountId: 3,
  accountUsername: "carol",
  factor: "password",
  event: "fail",
  detail: { reason: "bad_password" },
});
/** The system's own: no account, no address, nothing to open. */
const systemEvent = event(3, {
  factor: "session",
  event: "session_start",
});
/** Values the dashboard's vocabulary does not know, shown as they arrive. */
const unknownEvent = event(2, {
  accountId: 2,
  accountUsername: "bob",
  factor: "mock_unknown",
  event: "mock_event",
  ip: "198.51.100.9",
});

function page(items: unknown[], nextCursor = "") {
  return () => Response.json({ items, nextCursor });
}

function serve(
  list: (request: Request) => Response | Promise<Response>,
  extra: Record<
    string,
    (request: Request) => Response | Promise<Response>
  > = {},
): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${listPath}`]: list,
    [`GET ${accountsPath}`]: page([bob, carol]),
    [`GET ${accountsPath}/2`]: () => Response.json(bob),
    ...extra,
  });
}

async function mount(
  items: unknown[],
  path = "/admin/logs",
): Promise<{ api: FakeApi; router: ReturnType<typeof renderApp> }> {
  const api = serve(page(items));
  const router = renderApp(path, queryClient);
  await screen.findByRole("heading", { level: 1, name: "Logs" });
  return { api, router };
}

/** The filters the address carries. */
function addressOf(router: ReturnType<typeof renderApp>) {
  return Object.fromEntries(
    new URLSearchParams(router.state.location.searchStr),
  );
}

/** The body rows of the table, without the header. */
async function bodyRows(): Promise<HTMLElement[]> {
  const table = await screen.findByRole("table", { name: "Audit events" });
  return within(table).getAllByRole("row").slice(1);
}

function rowWith(rows: HTMLElement[], text: string): HTMLElement {
  const row = rows.find((candidate) => within(candidate).queryByText(text));
  if (row === undefined) throw new Error(`no row with ${text}`);
  return row;
}

describe("the page", () => {
  it("is headed Logs and says what it is for", async () => {
    await mount([passkeyUse]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
  });
});

describe("a row", () => {
  it("shows the time to the second, the account as a link, the category, the event and the address", async () => {
    await mount([passkeyUse]);
    const [row] = await bodyRows();

    const time = within(row as HTMLElement).getByText(
      /\d{2}\/\d{2}\/\d{4}, \d{2}:\d{2}:\d{2}/,
    );
    expect(time.tagName).toBe("TIME");
    expect(time).toHaveAttribute("datetime", "2026-09-25T10:15:30Z");
    expect(
      within(row as HTMLElement).getByRole("link", { name: "@bob" }),
    ).toHaveAttribute("href", "/admin/users/2");
    expect(within(row as HTMLElement).getByText("Passkey")).toBeVisible();
    expect(within(row as HTMLElement).getByText("Used")).toBeVisible();
    expect(within(row as HTMLElement).getByText("198.51.100.7")).toBeVisible();
  });

  it("labels an event with no account as No account rather than leaving a dash", async () => {
    await mount([systemEvent]);
    const [row] = await bodyRows();

    expect(within(row as HTMLElement).getByText("No account")).toBeVisible();
    expect(within(row as HTMLElement).queryByRole("link")).toBeNull();
  });

  it("marks a failure event as a failure and leaves the others plain", async () => {
    await mount([passwordFailed, passkeyUse]);
    const rows = await bodyRows();

    expect(within(rowWith(rows, "Failed")).getByText("Failed")).toHaveAttribute(
      "data-tone",
      "failure",
    );
    expect(within(rowWith(rows, "Used")).getByText("Used")).not.toHaveAttribute(
      "data-tone",
    );
  });

  it("shows a category and an event the vocabulary does not know exactly as they arrived", async () => {
    await mount([unknownEvent]);
    const [row] = await bodyRows();

    expect(
      within(row as HTMLElement).getByText("mock_unknown"),
    ).toHaveAttribute("data-raw");
    expect(within(row as HTMLElement).getByText("mock_event")).toHaveAttribute(
      "data-raw",
    );
  });

  it("gives every row the same leading column, a toggle or an inert spacer, so the times line up", async () => {
    await mount([passkeyUse, systemEvent, passwordFailed]);
    const rows = await bodyRows();
    expect(rows).toHaveLength(3);

    for (const row of rows) {
      const time = row.querySelector("time");
      const lead = time?.closest("span[id]")?.previousElementSibling;
      expect(lead).toBeTruthy();
      const hasDetail = within(row).queryByRole("button", {
        name: /^Show details/,
      });
      if (row === rows[1]) {
        expect(hasDetail).toBeNull();
        expect(lead).toHaveAttribute("aria-hidden", "true");
      } else {
        expect(lead).toBe(hasDetail);
      }
    }
  });
});

/** The value beside a detail list's term. */
async function termValue(term: string): Promise<HTMLElement> {
  await screen.findAllByText(term);
  const dt = screen
    .getAllByText(term)
    .map((element) => element.closest("dt"))
    .find((candidate) => candidate !== null);
  const dd = dt?.nextElementSibling;
  if (!(dd instanceof HTMLElement)) throw new Error(`no value for ${term}`);
  return dd;
}

describe("an opened row", () => {
  it("lists the full user agent, the address and the details, with a way to copy them", async () => {
    const user = userEvent.setup();
    await mount([passkeyUse]);
    const [row] = await bodyRows();

    await user.click(
      within(row as HTMLElement).getByRole("button", { name: /^Show details/ }),
    );

    expect(await termValue("User-Agent")).toHaveTextContent(
      "Mozilla/5.0 TestBrowser/1.0",
    );
    expect(await termValue("IP")).toHaveTextContent("198.51.100.7");
    const details = await termValue("Details");
    expect(details).toHaveTextContent('"kid": "key-1"');
    expect(within(details).getByRole("button", { name: "Copy" })).toBeVisible();
  });

  it("draws only the items an event carries", async () => {
    const user = userEvent.setup();
    await mount([passwordFailed]);
    const [row] = await bodyRows();

    await user.click(
      within(row as HTMLElement).getByRole("button", { name: /^Show details/ }),
    );

    expect(await termValue("Details")).toBeVisible();
    expect(screen.queryByText("User-Agent")).toBeNull();
    // The address is also a column, so only the detail list's own item is
    // absent; the event is repeated for a narrow screen, where its column is
    // the one scrolled away.
    expect(
      [...document.querySelectorAll("dt")].map((term) => term.textContent),
    ).toEqual(["Event", "Details"]);
  });
});

describe("the time range", () => {
  it("asks for the last day unless the address says otherwise", async () => {
    const { api } = await mount([passkeyUse]);

    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));
    const since = api.sent("GET", listPath)[0]?.url.searchParams.get("since");
    const age = Date.now() - Date.parse(since ?? "");
    expect(age).toBeGreaterThan(24 * 3600 * 1000 - 60_000);
    expect(age).toBeLessThan(24 * 3600 * 1000 + 60_000);
    expect(
      screen.getByRole("combobox", { name: "Time range" }),
    ).toHaveTextContent("Last 24 hours");
  });

  it("keeps a chosen preset in the address and asks again with it", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([passkeyUse]);

    await user.click(screen.getByRole("combobox", { name: "Time range" }));
    await user.click(
      await screen.findByRole("option", { name: "Last 7 days" }),
    );

    await waitFor(() => expect(addressOf(router).range).toBe("7d"));
    await waitFor(() => {
      const since = api
        .sent("GET", listPath)
        .at(-1)
        ?.url.searchParams.get("since");
      const age = Date.now() - Date.parse(since ?? "");
      expect(age).toBeGreaterThan(7 * 24 * 3600 * 1000 - 60_000);
    });
  });

  it("sends no time bound for all time", async () => {
    const { api } = await mount([passkeyUse], "/admin/logs?range=all");

    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));
    expect(api.sent("GET", listPath)[0]?.url.searchParams.has("since")).toBe(
      false,
    );
  });
});

describe("a custom range", () => {
  it("covers whole local days from the address", async () => {
    const { api } = await mount(
      [passkeyUse],
      "/admin/logs?range=custom&from=2026-09-01&to=2026-09-03",
    );

    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));
    const params = api.sent("GET", listPath)[0]?.url.searchParams;
    expect(params?.get("since")).toBe(new Date(2026, 8, 1).toISOString());
    expect(params?.get("until")).toBe(
      new Date(2026, 8, 3, 23, 59, 59, 999).toISOString(),
    );
    expect(
      screen.getByRole("button", { name: /Sep 1.*3, 2026/ }),
    ).toBeVisible();
  });

  it("says a start and an end are needed, and asks for nothing, until both are chosen", async () => {
    const user = userEvent.setup();
    const { api } = await mount([passkeyUse]);
    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));

    await user.click(screen.getByRole("combobox", { name: "Time range" }));
    await user.click(
      await screen.findByRole("option", { name: "Custom range" }),
    );

    expect(
      await screen.findByText("Choose a start and an end date"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: "Choose dates" })).toBeVisible();
    expect(api.sent("GET", listPath)).toHaveLength(1);
    // The earlier answer must not keep standing in for the unfinished one.
    expect(screen.queryByText("Passkey")).toBeNull();
  });

  it("says when the start is after the end, and asks for nothing", async () => {
    const { api } = await mount(
      [passkeyUse],
      "/admin/logs?range=custom&from=2026-09-03&to=2026-09-01",
    );

    expect(
      await screen.findByText("The start date is after the end date"),
    ).toBeVisible();
    expect(api.sent("GET", listPath)).toHaveLength(0);
  });

  it("takes both days from the calendar and keeps them in the address", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount(
      [passkeyUse],
      "/admin/logs?range=custom",
    );
    await screen.findByText("Choose a start and an end date");

    await user.click(screen.getByRole("button", { name: "Choose dates" }));
    const dialog = await screen.findByRole("dialog", { name: "Dates" });
    await user.click(within(dialog).getByRole("button", { name: / 12th, / }));
    await user.click(within(dialog).getByRole("button", { name: / 14th, / }));

    const now = new Date();
    const month = String(now.getMonth() + 1).padStart(2, "0");
    await waitFor(() =>
      expect(addressOf(router)).toMatchObject({
        range: "custom",
        from: `${now.getFullYear()}-${month}-12`,
        to: `${now.getFullYear()}-${month}-14`,
      }),
    );
    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));
  });

  it("clears the days again from the calendar", async () => {
    const user = userEvent.setup();
    const { router } = await mount(
      [passkeyUse],
      "/admin/logs?range=custom&from=2026-09-01&to=2026-09-03",
    );

    await user.click(screen.getByRole("button", { name: /Sep 1.*3, 2026/ }));
    const dialog = await screen.findByRole("dialog", { name: "Dates" });
    await user.click(
      within(dialog).getByRole("button", { name: "Clear dates" }),
    );

    expect(
      await screen.findByText("Choose a start and an end date"),
    ).toBeVisible();
    expect(addressOf(router).from ?? "").toBe("");
  });
});

describe("the filters", () => {
  async function openFilters(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: /^Filters/ }));
    return await screen.findByRole("dialog", { name: "Filters" });
  }

  it("counts the filters set on the Filters button", async () => {
    await mount(
      [passkeyUse],
      "/admin/logs?factor=totp&event=use&account=3&range=1h",
    );

    expect(
      screen.getByRole("button", { name: /^Filters/ }),
    ).toHaveAccessibleName("Filters, 3 active");
  });

  it("filters by category on the server and keeps it in the address", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([passkeyUse]);
    const dialog = await openFilters(user);

    await user.click(
      within(dialog).getByRole("combobox", { name: "Category" }),
    );
    await user.click(await screen.findByRole("option", { name: "Password" }));

    await waitFor(() => expect(addressOf(router).factor).toBe("password"));
    await waitFor(() =>
      expect(
        api.sent("GET", listPath).at(-1)?.url.searchParams.get("factor"),
      ).toBe("password"),
    );
  });

  it("offers every event whichever category is chosen, since the vocabulary has no pairing", async () => {
    const user = userEvent.setup();
    await mount([passkeyUse], "/admin/logs?factor=password");
    const dialog = await openFilters(user);

    await user.click(within(dialog).getByRole("combobox", { name: "Event" }));

    expect(
      await screen.findByRole("option", { name: "Rotated" }),
    ).toBeVisible();
    expect(screen.getByRole("option", { name: "Failed" })).toBeVisible();
  });

  it("filters by event on the server", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([passkeyUse]);
    const dialog = await openFilters(user);

    await user.click(within(dialog).getByRole("combobox", { name: "Event" }));
    await user.click(await screen.findByRole("option", { name: "Failed" }));

    await waitFor(() => expect(addressOf(router).event).toBe("fail"));
    await waitFor(() =>
      expect(
        api.sent("GET", listPath).at(-1)?.url.searchParams.get("event"),
      ).toBe("fail"),
    );
  });

  it("reads the directory only once the account list is opened, then filters by the account chosen", async () => {
    const user = userEvent.setup();
    const { api, router } = await mount([passkeyUse]);
    const dialog = await openFilters(user);
    expect(api.sent("GET", accountsPath)).toHaveLength(0);

    await user.click(within(dialog).getByRole("combobox", { name: "Account" }));
    await user.click(await screen.findByRole("option", { name: /Bob Stone/ }));

    await waitFor(() => expect(addressOf(router).account).toBe("2"));
    await waitFor(() =>
      expect(
        api.sent("GET", listPath).at(-1)?.url.searchParams.get("accountId"),
      ).toBe("2"),
    );
  });

  it("names the account the address carries even though the search has not listed it", async () => {
    const user = userEvent.setup();
    await mount([passkeyUse], "/admin/logs?account=2");
    const dialog = await openFilters(user);

    await waitFor(() =>
      expect(
        within(dialog).getByRole("combobox", { name: "Account" }),
      ).toHaveTextContent("Bob Stone"),
    );
  });

  it("reports a failed directory read once, inside the filter, with a way to ask again", async () => {
    const user = userEvent.setup();
    let failing = true;
    serve(page([passkeyUse]), {
      [`GET ${accountsPath}`]: () =>
        failing
          ? apiError("internal_error", 500)
          : Response.json({ items: [bob], nextCursor: "" }),
    });
    renderApp("/admin/logs", queryClient);
    await screen.findByRole("heading", { level: 1, name: "Logs" });
    const dialog = await openFilters(user);

    await user.click(within(dialog).getByRole("combobox", { name: "Account" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("The accounts could not be loaded");
    expect(notifyError).not.toHaveBeenCalled();

    failing = false;
    await user.click(within(alert).getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(within(dialog).queryByRole("alert")).toBeNull());
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("clearing the filters", () => {
  it("is offered only while a filter is set, and leaves the time range alone", async () => {
    const user = userEvent.setup();
    const { router } = await mount(
      [passkeyUse],
      "/admin/logs?range=7d&factor=password&event=fail&account=2",
    );

    await user.click(screen.getByRole("button", { name: "Clear filters" }));

    await waitFor(() =>
      expect(addressOf(router)).toMatchObject({ range: "7d" }),
    );
    const address = addressOf(router);
    expect(address.factor ?? "").toBe("");
    expect(address.event ?? "").toBe("");
    expect(address.account).toBeUndefined();
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Clear filters" }),
      ).toBeNull(),
    );
  });

  it("is not on the page while nothing is filtered", async () => {
    await mount([passkeyUse]);

    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });
});

describe("refreshing", () => {
  it("moves a preset on to the present, since it is pinned to when it was chosen", async () => {
    const user = userEvent.setup();
    vi.useFakeTimers({
      toFake: ["Date"],
      now: Date.parse("2026-09-25T12:00:00Z"),
    });
    const { api } = await mount([passkeyUse]);
    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));
    const first = api.sent("GET", listPath)[0]?.url.searchParams.get("since");

    vi.setSystemTime(Date.parse("2026-09-25T12:30:00Z"));
    await user.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(2));
    const second = api.sent("GET", listPath)[1]?.url.searchParams.get("since");
    expect(Date.parse(second ?? "") - Date.parse(first ?? "")).toBe(
      30 * 60_000,
    );
  });

  it("asks all time again from the first page", async () => {
    const user = userEvent.setup();
    const { api } = await mount([passkeyUse], "/admin/logs?range=all");
    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(1));

    await user.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() => expect(api.sent("GET", listPath)).toHaveLength(2));
    expect(api.sent("GET", listPath)[1]?.url.searchParams.has("cursor")).toBe(
      false,
    );
  });
});

describe("paging", () => {
  const original = globalThis.IntersectionObserver;
  afterEach(() => {
    globalThis.IntersectionObserver = original;
  });

  it("keeps a later-page failure from all time out of an unfinished custom range", async () => {
    const user = userEvent.setup();
    const api = serve((request) =>
      new URL(request.url).searchParams.get("cursor") === "c2"
        ? apiError("internal_error", 500)
        : Response.json({ items: [passkeyUse], nextCursor: "c2" }),
    );
    renderApp("/admin/logs?range=all", queryClient);
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(
      await screen.findByText("More audit log entries could not be loaded."),
    ).toBeVisible();
    const sent = api.sent("GET", listPath).length;

    await user.click(screen.getByRole("combobox", { name: "Time range" }));
    await user.click(
      await screen.findByRole("option", { name: "Custom range" }),
    );

    expect(
      await screen.findByText("Choose a start and an end date"),
    ).toBeVisible();
    expect(
      screen.queryByText("More audit log entries could not be loaded."),
    ).toBeNull();
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
    expect(api.sent("GET", listPath)).toHaveLength(sent);
  });

  it("asks for the next page with the cursor the server issued", async () => {
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
      new URL(request.url).searchParams.get("cursor") === "c2"
        ? Response.json({ items: [passwordFailed], nextCursor: "" })
        : Response.json({ items: [passkeyUse], nextCursor: "c2" }),
    );
    renderApp("/admin/logs", queryClient);

    expect(await screen.findByText("Failed")).toBeVisible();
    expect(api.sent("GET", listPath)[1]?.url.searchParams.get("cursor")).toBe(
      "c2",
    );
  });
});

describe("the states", () => {
  it("says there are no events in the range", async () => {
    await mount([]);

    expect(
      await screen.findByText("No events in this time range"),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Clear filters" })).toBeNull();
  });

  it("offers the way back out when filters are what emptied it", async () => {
    await mount([], "/admin/logs?factor=totp");

    expect(
      await screen.findByText("No events in this time range"),
    ).toBeVisible();
    expect(
      within(await screen.findByRole("table")).getByRole("button", {
        name: "Clear filters",
      }),
    ).toBeVisible();
  });

  it("reports a failed read once, keeps the toolbar, and reads again on Refresh", async () => {
    const user = userEvent.setup();
    let failing = true;
    serve(() =>
      failing
        ? apiError("internal_error", 500)
        : Response.json({ items: [passkeyUse], nextCursor: "" }),
    );
    renderApp("/admin/logs", queryClient);
    await screen.findByRole("heading", { level: 1, name: "Logs" });

    const alert = await screen.findByRole("alert");
    expect(alert).toBeVisible();
    expect(screen.getAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No events in this time range")).toBeNull();
    expect(screen.getByRole("button", { name: /^Filters/ })).toBeVisible();

    failing = false;
    await user.click(screen.getByRole("button", { name: "Refresh" }));

    expect(await screen.findByText("Passkey")).toBeVisible();
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull());
  });
});
