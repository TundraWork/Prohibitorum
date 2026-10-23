import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
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
    expect(await screen.findByText(/does not send mail/)).toBeVisible();
    expect(
      screen.queryByText(/You have no primary address/),
    ).not.toBeInTheDocument();
  });

  it("shows the no-mail notice with no addresses at all", async () => {
    mount([], false);
    expect(await screen.findByText(/does not send mail/)).toBeVisible();
  });

  it("sends the code from the row before opening the code dialog", async () => {
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
    // No dialog exists before the code is on its way.
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", {
        name: "More actions for spare@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(
          ([request]) =>
            request.method === "POST" &&
            new URL(request.url).pathname === `${emailsPath}/3/send-code`,
        ),
      ).toBe(true),
    );
    // The send succeeded, so the code dialog opens on the same single code.
    expect(
      await within(screen.getByRole("dialog")).findByText(
        /was sent to spare@example.test/,
      ),
    ).toBeVisible();
    expect(fetchBoundary.mock.calls).toHaveLength(1);
  });

  it("keeps the row's dialog closed and shows the error when the send fails", async () => {
    const user = userEvent.setup();
    mount(
      [
        emailFixture({
          id: 4,
          address: "bad@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        if (new URL(request.url).pathname === `${emailsPath}/4/send-code`) {
          return Response.json(
            { code: "email_delivery_failed", requestId: "r1" },
            { status: 502 },
          );
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", { name: "More actions for bad@example.test" }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Verify" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(
      /could not be sent/,
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
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

  it("disables Verify and shows the row sending while the code is on its way", async () => {
    const user = userEvent.setup();
    let releaseSend: ((value: Response) => void) | undefined;
    mount(
      [
        emailFixture({
          id: 7,
          address: "slow@example.test",
          primary: false,
          verified: false,
          verifiedVia: undefined,
          verifiedAt: undefined,
        }),
      ],
      true,
      (request) => {
        if (new URL(request.url).pathname === `${emailsPath}/7/send-code`) {
          // Held open until the test lets it go, so the pending state is on
          // screen to be read.
          return new Promise<Response>((resolve) => {
            releaseSend = resolve;
          });
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", {
        name: "More actions for slow@example.test",
      }),
    );
    const verifyItem = await screen.findByRole("menuitem", { name: "Verify" });
    await user.click(verifyItem);

    // The menu closes on click; the row itself says the code is leaving.
    expect(await screen.findByRole("status")).toHaveTextContent(
      /Sending the code/,
    );
    // A reopened menu's Verify is inert until the send answers.
    await user.click(
      screen.getByRole("button", {
        name: "More actions for slow@example.test",
      }),
    );
    const item = await screen.findByRole("menuitem", { name: "Verify" });
    expect(item).toHaveAttribute("aria-disabled", "true");
    await user.click(
      screen.getByRole("button", {
        name: "More actions for slow@example.test",
      }),
    );

    releaseSend?.(
      Response.json({ resendAfterSeconds: 60, expiresInSeconds: 900 }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(
      await within(dialog).findByText(/was sent to slow@example.test/),
    ).toBeVisible();
  });
});
