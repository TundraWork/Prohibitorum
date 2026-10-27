import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import { AccountSessionsSection } from "@/pages/admin/users/AccountSessionsSection";
import { accessHarness, refused } from "@/test/account-access";

type Session = components["schemas"]["SessionListItem"];

const firefox =
  "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0";
const safari =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const unnameable =
  "managed-device-agent; build 20260314.1; os ExampleOS 9.2; device laptop-0042";

const session = (overrides: Partial<Session>): Session => ({
  id: "s-1",
  isCurrent: false,
  issuedAt: new Date(Date.now() - 2 * 86_400_000).toISOString(),
  expiresAt: new Date(Date.now() + 86_400_000).toISOString(),
  lastSeenIp: "192.0.2.10",
  userAgent: firefox,
  ...overrides,
});

const path = "/api/prohibitorum/accounts/2/sessions";

describe("the sessions section", () => {
  const harness = accessHarness();
  const serve =
    (
      items: Session[],
      extra?: (request: Request) => Response | Promise<Response> | undefined,
    ) =>
    (request: Request) => {
      const canned = extra?.(request);
      if (canned) return canned;
      if (request.method === "GET" && request.url.endsWith(path)) {
        return Response.json({ items, nextCursor: "" });
      }
      return undefined;
    };

  it("names the device from its user agent, with the address and when it started", async () => {
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([
        session({}),
        session({
          id: "s-2",
          userAgent: unnameable,
          lastSeenIp: "198.51.100.4",
        }),
      ]),
    );

    const list = await screen.findByRole("list", { name: "Active sessions" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("Firefox · Linux");
    expect(rows[0]).toHaveTextContent("192.0.2.10");
    expect(rows[0]).toHaveTextContent("Started 2 days ago");
    // A user agent the title cannot name is shown whole.
    expect(rows[1]).toHaveTextContent(unnameable);
    expect(rows[1]).toHaveTextContent("198.51.100.4");
  });

  it("says so when the account has none, and offers no sign out everywhere", async () => {
    harness.mount(<AccountSessionsSection accountId={2} />, serve([]));

    expect(await screen.findByText("No active sessions")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Sign out everywhere" }),
    ).toBeNull();
  });

  it("offers sign out everywhere only with more than one session", async () => {
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([session({})]),
    );
    await screen.findByRole("list", { name: "Active sessions" });
    expect(
      screen.queryByRole("button", { name: "Sign out everywhere" }),
    ).toBeNull();
  });

  it("tells a failed read once, in the section, and not as a toast", async () => {
    harness.mount(<AccountSessionsSection accountId={2} />, () => refused());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
  });

  it("ends one session only after the confirmation, then reads the list again", async () => {
    const user = userEvent.setup();
    let reads = 0;
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve(
        [session({}), session({ id: "s-2", userAgent: safari })],
        (request) => {
          if (request.method === "GET") reads += 1;
          return undefined;
        },
      ),
    );

    await user.click(
      await screen.findByRole("button", {
        name: "End the session on Safari · iOS",
      }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "End this session?",
    });
    expect(within(dialog).getByText("Safari · iOS")).toBeVisible();
    expect(harness.sent("POST", "/sessions/revoke")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "End session" }),
    );

    await waitFor(() =>
      expect(harness.sent("POST", "/sessions/revoke")).toHaveLength(1),
    );
    expect(await harness.sent("POST", "/sessions/revoke")[0]?.json()).toEqual({
      id: "s-2",
    });
    await waitFor(() => expect(reads).toBe(2));
  });

  it("signs the account out everywhere after the confirmation", async () => {
    const user = userEvent.setup();
    let reads = 0;
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([session({}), session({ id: "s-2" })], (request) => {
        if (request.method === "GET") reads += 1;
        if (request.url.endsWith("/accounts/revoke-sessions")) {
          return Response.json({ revoked: 2 });
        }
        return undefined;
      }),
    );

    await user.click(
      await screen.findByRole("button", { name: "Sign out everywhere" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Sign this account out everywhere?",
    });
    expect(harness.sent("POST", "/accounts/revoke-sessions")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Sign out everywhere" }),
    );

    await waitFor(() =>
      expect(harness.sent("POST", "/accounts/revoke-sessions")).toHaveLength(1),
    );
    expect(
      await harness.sent("POST", "/accounts/revoke-sessions")[0]?.json(),
    ).toEqual({ id: 2 });
    await waitFor(() => expect(reads).toBe(2));
  });

  it("spins only the confirmation while signing out everywhere", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([session({}), session({ id: "s-2" })], (request) =>
        request.url.endsWith("/accounts/revoke-sessions")
          ? new Promise<Response>((resolve) => {
              release = resolve;
            })
          : undefined,
      ),
    );

    const trigger = await screen.findByRole("button", {
      name: "Sign out everywhere",
    });
    await user.click(trigger);
    const dialog = await screen.findByRole("alertdialog");
    const confirm = within(dialog).getByRole("button", {
      name: "Sign out everywhere",
    });
    await user.click(confirm);
    await waitFor(() =>
      expect(harness.sent("POST", "/accounts/revoke-sessions")).toHaveLength(1),
    );

    const spinners = document.querySelectorAll('[data-slot="spinner"]');
    expect(spinners).toHaveLength(1);
    expect(confirm).toContainElement(spinners[0] as HTMLElement);
    expect(trigger).toBeDisabled();
    release(Response.json({ revoked: 2 }));
  });

  it("reports a refused end once, inline, and closes the question", async () => {
    const user = userEvent.setup();
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([session({})], (request) =>
        request.method === "POST" ? refused() : undefined,
      ),
    );

    await user.click(
      await screen.findByRole("button", {
        name: "End the session on Firefox · Linux",
      }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "End session" }),
    );

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("reports a refused sign out everywhere once, inline", async () => {
    const user = userEvent.setup();
    harness.mount(
      <AccountSessionsSection accountId={2} />,
      serve([session({}), session({ id: "s-2" })], (request) =>
        request.method === "POST" ? refused() : undefined,
      ),
    );

    await user.click(
      await screen.findByRole("button", { name: "Sign out everywhere" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Sign out everywhere" }),
    );

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });
});
