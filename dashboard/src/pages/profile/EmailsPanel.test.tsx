import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { myEmailsQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { type EmailAddress, EmailsPanel } from "@/pages/profile/EmailsPanel";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  notifySuccess.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function emailFixture(overrides: Partial<EmailAddress> = {}): EmailAddress {
  return {
    id: 1,
    address: "alice@example.test",
    verified: true,
    verifiedVia: "code",
    primary: true,
    createdAt: "2026-01-01T00:00:00Z",
    verifiedAt: "2026-01-01T01:00:00Z",
    ...overrides,
  };
}

function mount(
  emails: EmailAddress[],
  deliveryAvailable = true,
  answer?: (request: Request) => Response | Promise<Response> | undefined,
) {
  queryClient.setQueryData(myEmailsQueryOptions().queryKey, {
    emails,
    deliveryAvailable,
    limit: 10,
  });
  fetchBoundary.mockImplementation(async (request) => {
    const canned = answer?.(request);
    if (canned) return canned;
    return new Response(null, { status: 204 });
  });
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <EmailsPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const emailsPath = "/api/prohibitorum/me/emails";

describe("EmailsPanel", () => {
  it("renders the primary row without actions and secondary rows with the menu", async () => {
    mount([
      emailFixture(),
      emailFixture({
        id: 2,
        address: "alice@work.example",
        primary: false,
        verifiedVia: "upstream",
        verifiedProvider: "Example IdP",
      }),
    ]);
    expect(await screen.findByText("alice@example.test")).toBeVisible();
    expect(screen.getByText("alice@work.example")).toBeVisible();
    expect(screen.getByText("Primary")).toBeVisible();
    // Only the secondary row carries a row menu.
    expect(
      screen.getByRole("button", {
        name: "More actions for alice@work.example",
      }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", {
        name: "More actions for alice@example.test",
      }),
    ).not.toBeInTheDocument();
  });

  it("shows the empty state when the account has no addresses", async () => {
    mount([]);
    expect(await screen.findByText("No addresses yet")).toBeVisible();
  });

  it("warns when no address is primary", async () => {
    mount([emailFixture({ primary: false })]);
    expect(
      await screen.findByText(/You have no primary address/),
    ).toBeVisible();
  });

  it("shows the no-mail notice instead when delivery is unavailable", async () => {
    mount([emailFixture({ primary: false })], false);
    expect(await screen.findByText(/does not send email/)).toBeVisible();
    expect(
      screen.queryByText(/You have no primary address/),
    ).not.toBeInTheDocument();
  });

  it("shows the no-mail notice with no addresses at all", async () => {
    mount([], false);
    expect(await screen.findByText(/does not send email/)).toBeVisible();
  });

  it("opens the code dialog from the row menu and sends the code on open", async () => {
    const user = userEvent.setup();
    mount(
      [
        emailFixture({
          id: 3,
          address: "spare@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        const path = new URL(request.url).pathname;
        if (path === `${emailsPath}/3/send-code`) {
          return Response.json({
            resendAfterSeconds: 60,
            expiresInSeconds: 900,
          });
        }
        return undefined;
      },
    );
    // No dialog exists before the menu's Verify is clicked.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "More actions for spare@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    // The menu item opens the dialog at once; the dialog itself sends the
    // code, so exactly one send goes out.
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText(/was sent to spare@example.test/),
    ).toBeVisible();
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(
          ([request]) =>
            request.method === "POST" &&
            new URL(request.url).pathname === `${emailsPath}/3/send-code`,
        ),
      ).toBe(true),
    );
    expect(fetchBoundary.mock.calls).toHaveLength(1);
  });

  it("counts the cooldown down in the dialog on a 429, without an error", async () => {
    const user = userEvent.setup();
    mount(
      [
        emailFixture({
          id: 4,
          address: "cool@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        if (new URL(request.url).pathname === `${emailsPath}/4/send-code`) {
          return new Response(
            JSON.stringify({ code: "rate_limited", requestId: "r1" }),
            { status: 429, headers: { "Retry-After": "30" } },
          );
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", {
        name: "More actions for cool@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    const dialog = await screen.findByRole("dialog");
    // A resend still inside its cooldown answers 429 with the wait: that is
    // the countdown, not a failure.
    expect(await within(dialog).findByText(/Resend in 30s/)).toBeVisible();
    expect(within(dialog).queryAllByRole("alert")).toHaveLength(0);
  });

  it("shows a real send failure inside the dialog, with a way to retry", async () => {
    const user = userEvent.setup();
    let failed = true;
    mount(
      [
        emailFixture({
          id: 5,
          address: "bad@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        if (new URL(request.url).pathname === `${emailsPath}/5/send-code`) {
          return failed
            ? new Response(null, { status: 503 })
            : Response.json({
                resendAfterSeconds: 60,
                expiresInSeconds: 900,
              });
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", {
        name: "More actions for bad@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    const dialog = await screen.findByRole("dialog");
    // The failure lands in the dialog, on the code step it belongs to. The
    // open dialog hides the page behind it from role queries, so the row's
    // cleanliness is asked of the document itself.
    const alerts = document.querySelectorAll('p[role="alert"]');
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent(/could not be sent/);
    expect(within(dialog).getByRole("alert")).toBe(alerts[0]);
    // The rows show nothing about the send: no status line, and the only
    // alert in the document is the dialog's.
    expect(document.querySelectorAll('[role="status"]')).toHaveLength(0);
    // And the dialog's own button is the way to retry the send: this one
    // goes out and answers with the cooldown again.
    failed = false;
    await user.click(within(dialog).getByRole("button", { name: "Resend" }));
    expect(await within(dialog).findByText(/Resend in 60s/)).toBeVisible();
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("adds, sends, and verifies through the two-step dialog", async () => {
    const user = userEvent.setup();
    mount([], true, (request) => {
      const path = new URL(request.url).pathname;
      if (request.method === "POST" && path === emailsPath) {
        return Response.json(
          emailFixture({
            id: 7,
            address: "new@example.test",
            verified: false,
            primary: false,
            verifiedVia: undefined,
            verifiedAt: undefined,
          }),
          { status: 201 },
        );
      }
      if (path === `${emailsPath}/7/send-code`) {
        return Response.json({
          resendAfterSeconds: 60,
          expiresInSeconds: 900,
        });
      }
      if (path === `${emailsPath}/7/verify`) {
        return Response.json(
          emailFixture({ id: 7, address: "new@example.test" }),
        );
      }
      return undefined;
    });
    await user.click(
      await screen.findByRole("button", { name: "Add address" }),
    );

    const dialog = await screen.findByRole("dialog");
    await user.type(
      within(dialog).getByLabelText("Address"),
      "new@example.test",
    );
    await user.click(within(dialog).getByRole("button", { name: "Send code" }));

    // Step two appears and mails the code.
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(
          ([request]) =>
            request.method === "POST" &&
            new URL(request.url).pathname === `${emailsPath}/7/send-code`,
        ),
      ).toBe(true),
    );
    expect(
      await within(screen.getByRole("dialog")).findByText(
        /was sent to new@example.test/,
      ),
    ).toBeVisible();

    // The add dialog must be gone: the code dialog never stacks on it
    // (review B7). The one open dialog is the code step, and no "Address"
    // field (the add dialog's only field) remains.
    const dialogs = screen.getAllByRole("dialog");
    expect(dialogs).toHaveLength(1);
    expect(screen.queryByLabelText("Address")).not.toBeInTheDocument();
  });

  it("refuses to delete the primary address and offers it for secondary ones", async () => {
    const user = userEvent.setup();
    mount([
      emailFixture(),
      emailFixture({ id: 2, address: "second@example.test", primary: false }),
    ]);
    // The primary row has no menu at all; the secondary row's menu opens.
    await user.click(
      screen.getByRole("button", {
        name: "More actions for second@example.test",
      }),
    );
    const menu = await screen.findByRole("menu");
    expect(within(menu).getByText("Make primary")).toBeVisible();
    expect(within(menu).getByText("Remove")).toBeVisible();
  });

  it("refetches the list after a delete, so the row leaves without a reload", async () => {
    const user = userEvent.setup();
    let deleted = false;
    mount(
      [
        emailFixture(),
        emailFixture({ id: 2, address: "second@example.test", primary: false }),
      ],
      true,
      (request) => {
        const path = new URL(request.url).pathname;
        if (request.method === "GET" && path === emailsPath) {
          // The read after the delete no longer lists the removed address.
          const rows = deleted
            ? [emailFixture()]
            : [
                emailFixture(),
                emailFixture({
                  id: 2,
                  address: "second@example.test",
                  primary: false,
                }),
              ];
          return Response.json({
            emails: rows,
            deliveryAvailable: true,
            limit: 10,
          });
        }
        if (request.method === "DELETE" && path === `${emailsPath}/2`) {
          deleted = true;
          return new Response(null, { status: 204 });
        }
        return undefined;
      },
    );

    await user.click(
      screen.getByRole("button", {
        name: "More actions for second@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }));
    await user.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Remove",
      }),
    );

    expect(await screen.findByText("second@example.test")).toBeVisible();
    // The list reads again once the delete answers, and the fresh answer —
    // which no longer carries the row — is what is on screen.
    await waitFor(() =>
      expect(screen.queryByText("second@example.test")).not.toBeInTheDocument(),
    );
    expect(deleted).toBe(true);
  });

  it("carries the row's next step in its menu, not as an inline button", async () => {
    const user = userEvent.setup();
    mount([
      emailFixture({
        id: 5,
        address: "fresh@example.test",
        primary: false,
        verified: false,
        verifiedVia: undefined,
        verifiedAt: undefined,
      }),
    ]);
    expect(
      screen.queryByRole("button", { name: "Verify" }),
    ).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "More actions for fresh@example.test",
      }),
    );
    const menu = await screen.findByRole("menu");
    expect(
      within(menu).getByRole("menuitem", { name: "Verify" }),
    ).toBeVisible();
    expect(
      within(menu).getByRole("menuitem", { name: "Remove" }),
    ).toBeVisible();
    // Verify sits above the destructive action, so the divider between them
    // is meaningful — and it is the menu's only divider.
    expect(within(menu).getAllByRole("separator")).toHaveLength(1);
    expect(within(menu).queryByText("Make primary")).not.toBeInTheDocument();
  });

  it("says an address without mail waits for an administrator, and keeps its menu to Remove alone", async () => {
    const user = userEvent.setup();
    mount(
      [
        emailFixture({
          id: 6,
          address: "quiet@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      false,
    );
    expect(
      await screen.findByText(/Waiting for an administrator/),
    ).toBeVisible();
    await user.click(
      screen.getByRole("button", {
        name: "More actions for quiet@example.test",
      }),
    );
    const menu = await screen.findByRole("menu");
    // Remove is the menu's only action, so no divider sits above it.
    expect(within(menu).getAllByRole("menuitem")).toHaveLength(1);
    expect(within(menu).queryByRole("separator")).not.toBeInTheDocument();
  });
  it("sends once per opening, under StrictMode and re-renders, and a reopen inside the cooldown counts down", async () => {
    const user = userEvent.setup();
    const sends: number[] = [];
    queryClient.setQueryData(myEmailsQueryOptions().queryKey, {
      emails: [
        emailFixture({
          id: 8,
          address: "once@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      deliveryAvailable: true,
      limit: 10,
    });
    fetchBoundary.mockImplementation(async (request) => {
      if (new URL(request.url).pathname === `${emailsPath}/8/send-code`) {
        sends.push(Date.now());
        // The first code goes out; any send after it is inside the server's
        // cooldown and answers 429 with the wait in Retry-After.
        return sends.length === 1
          ? Response.json({ resendAfterSeconds: 60, expiresInSeconds: 900 })
          : new Response(
              JSON.stringify({ code: "rate_limited", requestId: "r1" }),
              { status: 429, headers: { "Retry-After": "55" } },
            );
      }
      return new Response(null, { status: 204 });
    });
    const tree = () => (
      <StrictMode>
        <I18nProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>
            <EmailsPanel />
          </QueryClientProvider>
        </I18nProvider>
      </StrictMode>
    );
    const { rerender } = render(tree());
    const menu = await screen.findByRole("button", {
      name: "More actions for once@example.test",
    });
    // Nothing is sent while no dialog is open, re-render or not.
    rerender(tree());
    expect(sends).toHaveLength(0);

    await user.click(menu);
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    const dialog = await screen.findByRole("dialog");
    expect(await within(dialog).findByText(/Resend in 60s/)).toBeVisible();
    // A re-render of an open dialog does not send again.
    rerender(tree());
    expect(sends).toHaveLength(1);

    await user.click(
      within(dialog).getByRole("button", { name: "Verify later" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(sends).toHaveLength(1);

    // Reopening inside the cooldown sends once more; the 429 is the countdown.
    await user.click(
      screen.getByRole("button", {
        name: "More actions for once@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    const reopened = await screen.findByRole("dialog");
    expect(await within(reopened).findByText(/Resend in 55s/)).toBeVisible();
    expect(sends).toHaveLength(2);
    expect(document.querySelectorAll('[role="alert"]')).toHaveLength(0);
  });
  it("says in the dialog when the server could not deliver the code, and Resend tries again", async () => {
    const user = userEvent.setup();
    let sends = 0;
    mount(
      [
        emailFixture({
          id: 9,
          address: "dropped@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        if (new URL(request.url).pathname === `${emailsPath}/9/send-code`) {
          sends += 1;
          return sends === 1
            ? Response.json(
                { code: "email_delivery_failed", requestId: "r1" },
                { status: 502 },
              )
            : Response.json({ resendAfterSeconds: 60, expiresInSeconds: 900 });
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", {
        name: "More actions for dropped@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText(
        /The email with the code could not be delivered/,
      ),
    ).toBeVisible();
    await user.click(within(dialog).getByRole("button", { name: "Resend" }));
    expect(await within(dialog).findByText(/Resend in 60s/)).toBeVisible();
    expect(
      within(dialog).queryByText(/could not be delivered/),
    ).not.toBeInTheDocument();
    expect(sends).toBe(2);
  });
});
