import { I18nProvider } from "@lingui/react";
import type { QueryClient } from "@tanstack/react-query";
import { QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider as AriaI18nProvider } from "react-aria";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { AdminIdentityProviderNew } from "@/pages/admin/identity-providers/AdminIdentityProviderNew";
import { type FakeApi, fakeApi } from "@/test/app";

type Provider = components["schemas"]["IdentityProviderView"];

let queryClient: QueryClient;
let api: FakeApi;

beforeEach(() => {
  i18n.activate("en");
});

function created(overrides: Partial<Provider> = {}): Provider {
  return {
    slug: "corp",
    displayName: "Corp",
    protocol: "oidc",
    allowSignIn: true,
    pendingInvitationCount: 0,
    mode: "invite_only",
    disabled: false,
    ready: false,
    secretConfigured: false,
    secretStatus: "unconfigured",
    secretValidatedAt: null,
    createdAt: "2026-01-01T00:00:00Z",
    config: {},
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
    ...overrides,
  };
}

function mount() {
  queryClient = createQueryClient(vi.fn());
  render(
    <I18nProvider i18n={i18n}>
      <AriaI18nProvider>
        <QueryClientProvider client={queryClient}>
          <AdminIdentityProviderNew />
        </QueryClientProvider>
      </AriaI18nProvider>
    </I18nProvider>,
  );
}

/** One provisioning card; the chosen one is the checked radio. */
function modeRadio(name: string): HTMLElement {
  return screen.getByRole("radio", { name });
}

async function chooseProtocol(
  user: ReturnType<typeof userEvent.setup>,
  name: string,
) {
  await user.click(screen.getByRole("radio", { name: new RegExp(name) }));
}

describe("the new provider form's sign-in switch", () => {
  it("defaults the switch off for VRChat and on for the others, with the mode following", async () => {
    const user = userEvent.setup();
    mount();

    const switchEl = screen.getByRole("switch", { name: "Allow sign-in" });
    expect(switchEl).toBeChecked();
    expect(modeRadio("Creates accounts")).toBeChecked();

    await chooseProtocol(user, "VRChat");
    expect(
      screen.getByRole("switch", { name: "Allow sign-in" }),
    ).not.toBeChecked();
    expect(modeRadio("Invitation only")).toBeChecked();

    await chooseProtocol(user, "Steam");
    expect(screen.getByRole("switch", { name: "Allow sign-in" })).toBeChecked();
    expect(modeRadio("Creates accounts")).toBeChecked();
  });

  it("shows the weak-proof note while VRChat is chosen, whoever flips the switch", async () => {
    const user = userEvent.setup();
    mount();

    await chooseProtocol(user, "VRChat");
    expect(
      screen.getByText(/VRChat proves a profile through a link in its bio/),
    ).toBeInTheDocument();
    // The note stays even if the operator turns sign-in back on: what it
    // warns about is the proof, not the button.
    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    expect(
      screen.getByText(/VRChat proves a profile through a link in its bio/),
    ).toBeInTheDocument();
  });

  it("drops the creates-accounts mode when the switch turns off, resetting a live selection", async () => {
    const user = userEvent.setup();
    mount();

    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await waitFor(() => expect(modeRadio("Invitation only")).toBeChecked());
    expect(modeRadio("Creates accounts")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("sends allowSignIn and the reset mode in the create body", async () => {
    const user = userEvent.setup();
    api = fakeApi({
      "POST /api/prohibitorum/identity-providers": () =>
        Response.json(created({ allowSignIn: false, mode: "invite_only" })),
    });
    mount();

    await user.type(screen.getByLabelText("Name"), "Corp");
    await user.type(screen.getByLabelText("Identifier"), "corp");
    await user.type(screen.getByLabelText("Issuer URL"), "https://idp.example");
    await user.type(screen.getByLabelText("Client ID"), "client-1");
    // A public client needs no secret, so the body this test inspects is the
    // switch's, not the credential's.
    await user.click(
      screen.getByRole("combobox", { name: /Client authentication/ }),
    );
    const authMethods = await screen.findByRole("listbox");
    await user.click(within(authMethods).getByText("Public client"));

    await user.click(screen.getByRole("switch", { name: "Allow sign-in" }));
    await user.click(screen.getByRole("button", { name: "Add provider" }));
    await waitFor(() => {
      expect(
        api.sent("POST", "/api/prohibitorum/identity-providers"),
      ).toHaveLength(1);
    });
    const body = api.sent("POST", "/api/prohibitorum/identity-providers")[0]
      ?.body as { allowSignIn?: boolean; mode?: string };
    expect(body.allowSignIn).toBe(false);
    expect(body.mode).toBe("invite_only");
  });
});
