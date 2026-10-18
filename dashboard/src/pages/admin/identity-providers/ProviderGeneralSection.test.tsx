import { I18nProvider } from "@lingui/react";
import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider, useSuspenseQuery } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Suspense } from "react";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { identityProviderQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderGeneralSection } from "@/pages/admin/identity-providers/ProviderGeneralSection";
import { apiError, type FakeApi, fakeApi } from "@/test/app";

type Provider = components["schemas"]["IdentityProviderView"];

let queryClient: QueryClient;
let api: FakeApi;

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
    linkedAccountCount: 2,
    ...overrides,
  };
}

function mount(record: Provider) {
  queryClient = createQueryClient(vi.fn());
  queryClient.setQueryData(
    identityProviderQueryOptions(record.slug).queryKey,
    record,
  );
  render(
    <I18nProvider i18n={i18n}>
      <AriaI18nProvider>
        <QueryClientProvider client={queryClient}>
          <Suspense fallback={null}>
            <ProviderObserver slug={record.slug} />
          </Suspense>
          <ProviderGeneralSection provider={record} />
        </QueryClientProvider>
      </AriaI18nProvider>
    </I18nProvider>,
  );
}

function ProviderObserver({ slug }: { slug: string }) {
  useSuspenseQuery(identityProviderQueryOptions(slug));
  return null;
}

/** The provisioning select, named by its label; its text is the selection. */
function modeTrigger(): HTMLElement {
  return screen.getByRole("combobox", {
    name: /Provisioning/,
  });
}

/** Opens the provisioning select and picks an option by name. */
async function chooseMode(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(modeTrigger());
  const listbox = await screen.findByRole("listbox");
  await user.click(within(listbox).getByText(name));
}

describe("the provider general form's sign-in switch", () => {
  it("drops the creates-accounts mode and resets a live selection when sign-in turns off", async () => {
    const user = userEvent.setup();
    mount(provider({ mode: "auto_provision" }));

    await chooseMode(user, "Creates accounts");
    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));

    // Flipping the switch resets the stored mode immediately: the trigger now
    // announces invitation-only, and reopening the list offers no way back to
    // the mode that exists only to back a sign-in button.
    expect(modeTrigger()).toHaveTextContent("Invitation only");
    await user.click(modeTrigger());
    const options = await screen.findByRole("listbox");
    expect(
      within(options).queryByText("Creates accounts"),
    ).not.toBeInTheDocument();
    await user.click(within(options).getByText("Invitation only"));
  });

  it("confirms before turning sign-in off, and a cancelled dialog saves nothing", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "PUT /api/prohibitorum/identity-providers/corp": () =>
        Response.json(provider()),
    });
    mount(provider({ mode: "invite_only" }));

    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    const dialog = await screen.findByRole("alertdialog", {
      name: "Turn off sign-in?",
    });

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    });
    expect(
      api.sent("PUT", "/api/prohibitorum/identity-providers/corp"),
    ).toEqual([]);
  });

  it("saves with allowSignIn false after the dialog is confirmed", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "PUT /api/prohibitorum/identity-providers/corp": () =>
        Response.json(provider({ allowSignIn: false })),
    });
    mount(provider({ mode: "invite_only" }));

    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await user.click(screen.getByRole("button", { name: "Save" }));

    const dialog = await screen.findByRole("alertdialog", {
      name: "Turn off sign-in?",
    });
    await user.click(
      within(dialog).getByRole("button", { name: "Turn off sign-in" }),
    );

    await waitFor(() => {
      expect(
        api.sent("PUT", "/api/prohibitorum/identity-providers/corp"),
      ).toHaveLength(1);
    });
    const body = api.sent("PUT", "/api/prohibitorum/identity-providers/corp")[0]
      ?.body as { allowSignIn?: boolean; mode?: string };
    expect(body.allowSignIn).toBe(false);
    expect(body.mode).toBe("invite_only");
  });

  it("shows the VRChat weak-proof note only for the VRChat protocol", () => {
    const { unmount } = render(
      <I18nProvider i18n={i18n}>
        <AriaI18nProvider>
          <QueryClientProvider client={queryClient}>
            <ProviderGeneralSection
              provider={provider({ protocol: "vrchat" })}
            />
          </QueryClientProvider>
        </AriaI18nProvider>
      </I18nProvider>,
    );
    queryClient = createQueryClient(vi.fn());
    expect(
      screen.getByText(/VRChat proves a profile through a link in its bio/),
    ).toBeInTheDocument();
    unmount();

    mount(provider({ protocol: "oidc" }));
    expect(
      screen.queryByText(/VRChat proves a profile through a link in its bio/),
    ).not.toBeInTheDocument();
    // The generic off-hint stays for both protocols.
    expect(
      screen.getAllByText(/this provider can't be used to sign in/).length,
    ).toBeGreaterThan(0);
  });

  it("keeps a failed save on the form, not on a field", async () => {
    api = fakeApi({
      "PUT /api/prohibitorum/identity-providers/corp": () =>
        apiError("server_error", 500),
    });
    const user = userEvent.setup();
    mount(provider());
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => {
      expect(
        api.sent("PUT", "/api/prohibitorum/identity-providers/corp"),
      ).toHaveLength(1);
    });
    expect(screen.getByLabelText("Name")).toHaveValue("Corporate SSO");
  });
});
