import { I18nProvider } from "@lingui/react";
import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderDangerSection } from "@/pages/admin/identity-providers/ProviderDangerSection";
import { fakeApi } from "@/test/app";

type Provider = components["schemas"]["IdentityProviderView"];

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
});

function provider(overrides: Partial<Provider> = {}): Provider {
  return {
    slug: "corp",
    displayName: "Corporate SSO",
    protocol: "oidc",
    allowSignIn: true,
    pendingInvitationCount: 0,
    mode: "invite_only",
    disabled: false,
    ready: true,
    secretConfigured: true,
    secretStatus: "valid",
    secretValidatedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    config: {},
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
    ...overrides,
  };
}

function mount(record: Provider) {
  queryClient = createQueryClient(vi.fn());
  render(
    <I18nProvider i18n={i18n}>
      <AriaI18nProvider>
        <QueryClientProvider client={queryClient}>
          <ProviderDangerSection provider={record} />
        </QueryClientProvider>
      </AriaI18nProvider>
    </I18nProvider>,
  );
}

async function openDeleteDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Delete" }));
  return screen.findByRole("alertdialog", { name: "Delete this provider?" });
}

describe("the provider delete confirmation", () => {
  it("counts the pending invitations that die with the provider", async () => {
    const user = userEvent.setup();
    fakeApi({
      "DELETE /api/prohibitorum/identity-providers/corp": () =>
        new Response(null, { status: 204 }),
    });
    mount(
      provider({
        pendingInvitationCount: 3,
        linkedAccountCount: 2,
      }),
    );

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(/2 accounts lose the way they sign in/),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        /3 unfinished invitations whose identities were verified/,
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByText(/through it is deleted with the provider/),
    ).toBeInTheDocument();
  });

  it("uses the singular for exactly one pending invitation", async () => {
    const user = userEvent.setup();
    fakeApi({
      "DELETE /api/prohibitorum/identity-providers/corp": () =>
        new Response(null, { status: 204 }),
    });
    mount(provider({ pendingInvitationCount: 1 }));

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(
        /1 unfinished invitation whose identity was verified/,
      ),
    ).toBeInTheDocument();
  });

  it("says only what is removed when nothing rides on the provider", async () => {
    const user = userEvent.setup();
    fakeApi({
      "DELETE /api/prohibitorum/identity-providers/corp": () =>
        new Response(null, { status: 204 }),
    });
    mount(provider());

    const dialog = await openDeleteDialog(user);
    expect(
      within(dialog).getByText(/The provider and its icon are removed/),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByText(/unfinished invitation/),
    ).not.toBeInTheDocument();
  });
});
