import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { accountEmailsQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AccountEmailsSection } from "@/pages/admin/users/AccountEmailsSection";
import type { EmailAddress } from "@/pages/profile/EmailsPanel";

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

function fixture(overrides: Partial<EmailAddress> = {}): EmailAddress {
  return {
    id: 1,
    address: "member@example.test",
    verified: true,
    verifiedVia: "admin",
    verifiedBy: "root",
    primary: true,
    createdAt: "2026-01-01T00:00:00Z",
    verifiedAt: "2026-01-01T01:00:00Z",
    ...overrides,
  };
}

function mount(
  emails: EmailAddress[],
  answer?: (request: Request) => Response | Promise<Response> | undefined,
) {
  queryClient.setQueryData(accountEmailsQueryOptions(7).queryKey, {
    emails,
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
        <AccountEmailsSection accountId={7} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AccountEmailsSection", () => {
  it("shows the asserting admin on verified rows", async () => {
    mount([fixture()]);
    expect(await screen.findByText("member@example.test")).toBeVisible();
    expect(screen.getByText(/by root/)).toBeVisible();
  });

  it("asserts an unverified address through its confirmation", async () => {
    const user = userEvent.setup();
    mount(
      [
        fixture({
          id: 2,
          address: "spare@example.test",
          verified: false,
          primary: false,
          verifiedVia: undefined,
          verifiedBy: undefined,
          verifiedAt: undefined,
        }),
      ],
      (request) => {
        if (
          request.method === "POST" &&
          new URL(request.url).pathname ===
            "/api/prohibitorum/accounts/7/emails/2/verify"
        ) {
          return Response.json(
            fixture({ id: 2, address: "spare@example.test", verified: true }),
          );
        }
        return undefined;
      },
    );
    await user.click(
      screen.getByRole("button", {
        name: "More actions for spare@example.test",
      }),
    );
    await user.click(
      await screen.findByRole("menuitem", { name: /Confirm as verified/ }),
    );
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(/Confirm this address belongs/),
    ).toBeVisible();
    await user.click(
      within(dialog).getByRole("button", { name: "Confirm as verified" }),
    );
    await waitFor(() =>
      expect(
        fetchBoundary.mock.calls.some(
          ([request]) =>
            request.method === "POST" &&
            new URL(request.url).pathname ===
              "/api/prohibitorum/accounts/7/emails/2/verify",
        ),
      ).toBe(true),
    );
  });

  it("names the fallback address when removing the primary", async () => {
    const user = userEvent.setup();
    mount([
      fixture(),
      fixture({
        id: 2,
        address: "next@example.test",
        verifiedBy: undefined,
        primary: false,
      }),
    ]);
    await user.click(
      screen.getByRole("button", {
        name: "More actions for member@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(
      within(dialog).getByText(/next@example.test.*will become/),
    ).toBeVisible();
  });

  it("picks the earliest-verified address as the fallback, not the first listed", async () => {
    const user = userEvent.setup();
    // Listed with the later-verified one first, so array order would be wrong.
    mount([
      fixture(),
      fixture({
        id: 3,
        address: "late@example.test",
        primary: false,
        verifiedAt: "2026-03-01T00:00:00Z",
      }),
      fixture({
        id: 2,
        address: "early@example.test",
        primary: false,
        verifiedAt: "2026-02-01T00:00:00Z",
      }),
    ]);
    await user.click(
      screen.getByRole("button", {
        name: "More actions for member@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText(/early@example.test/)).toBeVisible();
    expect(
      within(dialog).queryByText(/late@example.test/),
    ).not.toBeInTheDocument();
  });

  it("says the member keeps no primary when nothing can take over", async () => {
    const user = userEvent.setup();
    mount([fixture()]);
    await user.click(
      screen.getByRole("button", {
        name: "More actions for member@example.test",
      }),
    );
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText(/no primary address/)).toBeVisible();
  });
});
