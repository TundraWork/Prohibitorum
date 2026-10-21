import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mailSettingsQueryOptions, myEmailsQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { MailPanel } from "@/pages/admin/settings/MailPanel";

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

function mount(configured: boolean) {
  queryClient.setQueryData(
    mailSettingsQueryOptions().queryKey,
    configured
      ? {
          configured: true,
          host: "smtp.example.test",
          port: 587,
          security: "starttls",
          from: "no-reply@example.test",
        }
      : { configured: false },
  );
  queryClient.setQueryData(myEmailsQueryOptions().queryKey, {
    emails: [
      {
        id: 1,
        address: "root@example.test",
        verified: true,
        verifiedVia: "code",
        primary: true,
        createdAt: "2026-01-01T00:00:00Z",
      },
    ],
    deliveryAvailable: configured,
    limit: 10,
  });
  fetchBoundary.mockImplementation(
    async () => new Response(null, { status: 204 }),
  );
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <MailPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("MailPanel", () => {
  it("shows the relay settings when mail is configured", async () => {
    mount(true);
    expect(await screen.findByText("smtp.example.test:587")).toBeVisible();
    expect(screen.getByText("STARTTLS")).toBeVisible();
    expect(screen.getByText("no-reply@example.test")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Send test mail" }),
    ).toBeVisible();
  });

  it("shows the unconfigured state without the test button", async () => {
    mount(false);
    expect(await screen.findByText("Off")).toBeVisible();
    expect(screen.getByText(/Fill in the server and sender/)).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Send test mail" }),
    ).not.toBeInTheDocument();
  });

  it("prefills the test dialog with the admin's primary address", async () => {
    const user = userEvent.setup();
    mount(true);
    await user.click(
      await screen.findByRole("button", { name: "Send test mail" }),
    );
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByDisplayValue("root@example.test")).toBeVisible();
  });
});
