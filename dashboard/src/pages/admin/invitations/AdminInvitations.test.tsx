import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
import { providerFixture } from "@/test/identity-provider";
import { invitationFixture } from "@/test/invitation";

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
const listPath = `${base}/invitations`;

const plain = invitationFixture({ token: "tok-plain" });
const bound = invitationFixture({
  token: "tok-bound",
  url: "https://test/enroll/tok-bound",
  role: "admin",
  username: "dana",
  expectedUpstreamIdpSlug: "corp",
  groupIds: [1, 2],
  groups: [{ id: 1, slug: "staff", displayName: "Staff" }],
});

function serve(
  list: (request: Request) => Response | Promise<Response>,
  extra: Record<string, () => Response | Promise<Response>> = {},
): FakeApi {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${listPath}`]: list,
    [`GET ${base}/identity-providers`]: () =>
      Response.json({ items: [providerFixture()] }),
    ...extra,
  });
}

const page =
  (items: unknown[], nextCursor = "") =>
  () =>
    Response.json({ items, nextCursor });

async function mount(api: FakeApi) {
  const router = renderApp("/admin/invitations", queryClient);
  await screen.findByRole("heading", { level: 1, name: "Invitations" });
  return { api, router };
}

describe("the page", () => {
  it("says the list holds only waiting invitations and offers Invite a user", async () => {
    await mount(serve(page([plain])));

    expect(
      screen.getByText(
        "Invitations waiting to be used. Used or expired ones leave this list.",
      ),
    ).toBeVisible();
    expect(screen.getByRole("link", { name: "Invite a user" })).toHaveAttribute(
      "href",
      "/admin/invitations/new",
    );
  });

  it("explains an empty list with one help line", async () => {
    await mount(serve(page([])));

    expect(await screen.findByText("No invitations waiting")).toBeVisible();
    expect(
      screen.getByText(/Invite someone and their link appears here/),
    ).toBeVisible();
  });

  it("tells a failed read in the page and draws no table", async () => {
    serve(() => apiError("internal", 500));
    renderApp("/admin/invitations", queryClient);
    await screen.findByRole("heading", { level: 1, name: "Invitations" });

    expect(await screen.findByRole("alert")).toBeVisible();
    expect(screen.queryByRole("table")).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("a row", () => {
  it("names the invitee, marks an administrator and resolves the provider and groups", async () => {
    await mount(serve(page([plain, bound])));
    const table = await screen.findByRole("table", { name: "Invitations" });

    const row = within(table).getByText("dana").closest("tr");
    if (row === null) throw new Error("no row");
    expect(within(row).getByText("Admin")).toBeVisible();
    expect(within(row).getByText("Corporate SSO")).toBeVisible();
    expect(within(row).getByText(/Staff/)).toBeVisible();
    expect(
      within(row).getByText("(some groups no longer exist)"),
    ).toBeVisible();
  });

  it("says a missing username is chosen at registration and shows no provider", async () => {
    await mount(serve(page([plain])));
    const table = await screen.findByRole("table", { name: "Invitations" });

    expect(within(table).getByText("Chosen at registration")).toBeVisible();
    expect(within(table).queryByText("Admin")).toBeNull();
  });
});

describe("paging", () => {
  const original = globalThis.IntersectionObserver;
  afterEach(() => {
    globalThis.IntersectionObserver = original;
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
        ? Response.json({ items: [bound], nextCursor: "" })
        : Response.json({ items: [plain], nextCursor: "c2" }),
    );
    renderApp("/admin/invitations", queryClient);

    expect(await screen.findByText("dana")).toBeVisible();
    expect(api.sent("GET", listPath)[1]?.url.searchParams.get("cursor")).toBe(
      "c2",
    );
  });
});

describe("copying the link", () => {
  it("puts the registration link on the clipboard", async () => {
    const user = userEvent.setup();
    await mount(serve(page([plain])));
    const writeText = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue();

    await user.click(
      await screen.findByRole("button", {
        name: /^Copy the registration link/,
      }),
    );

    expect(writeText).toHaveBeenCalledWith("https://test/enroll/tok-1");
  });

  it("says so when the browser refuses the clipboard", async () => {
    const user = userEvent.setup();
    await mount(serve(page([plain])));
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new Error("blocked"),
    );

    await user.click(
      await screen.findByRole("button", {
        name: /^Copy the registration link/,
      }),
    );

    expect(await screen.findByText(/Could not copy the link/)).toBeVisible();
  });
});

describe("revoking", () => {
  it("confirms, sends the token and refreshes the list", async () => {
    const user = userEvent.setup();
    let remaining = [plain, bound];
    const api = serve(
      () => Response.json({ items: remaining, nextCursor: "" }),
      {
        [`POST ${listPath}/revoke`]: () => {
          remaining = [bound];
          return Response.json({});
        },
      },
    );
    await mount(api);

    await user.click(
      (
        await screen.findAllByRole("button", { name: /^Revoke/ })
      )[0] as HTMLElement,
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Revoke this invitation?")).toBeVisible();
    expect(api.sent("POST", `${listPath}/revoke`)).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Revoke" }));

    await waitFor(() => {
      expect(api.sent("POST", `${listPath}/revoke`)[0]?.body).toEqual({
        token: "tok-plain",
      });
    });
    // The list is read again, so the revoked row leaves it.
    await waitFor(() => {
      expect(api.sent("GET", listPath).length).toBeGreaterThanOrEqual(2);
      expect(screen.queryByText("Chosen at registration")).toBeNull();
    });
  });

  it("sends nothing when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    const api = serve(page([plain]));
    await mount(api);

    await user.click(await screen.findByRole("button", { name: /^Revoke/ }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    expect(api.sent("POST", `${listPath}/revoke`)).toHaveLength(0);
  });

  it("reports a failed revoke once, in the page", async () => {
    const user = userEvent.setup();
    const api = serve(page([plain]), {
      [`POST ${listPath}/revoke`]: () => apiError("invitation_not_found", 404),
    });
    await mount(api);

    await user.click(await screen.findByRole("button", { name: /^Revoke/ }));
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Revoke" }));

    expect(await screen.findByRole("alert")).toBeVisible();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
