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
});
