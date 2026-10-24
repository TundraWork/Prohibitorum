import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { ProviderGeneralSection } from "@/pages/admin/identity-providers/ProviderGeneralSection";
import {
  type Provider,
  providerFixture,
  publicOidcConfig,
} from "@/test/identity-provider";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

function mount(
  provider: Provider,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") return Response.json(provider);
    return new Response(null, { status: 204 });
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <ProviderGeneralSection provider={provider} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const storedConfig = expect.objectContaining({
  issuerUrl: "https://idp.example.com",
  clientId: "client",
});
const signInSwitch = () =>
  screen.getByRole("switch", { name: "Allow sign-in" });
const mode = (name: string) => screen.getByRole("radio", { name });
const lockout =
  "Anyone who signs in only through this provider will be locked out until an administrator sends them a registration link.";

describe("the sign-in section", () => {
  it("has its own heading, no name field, and no footer until something changes", () => {
    mount(providerFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Sign-in" }),
    ).toBeVisible();
    expect(signInSwitch()).toBeChecked();
    expect(screen.queryByRole("textbox", { name: "Name" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("offers the three provisioning modes as described cards, the saved one chosen", () => {
    mount(providerFixture({ mode: "invite_only" }));

    const group = screen.getByRole("radiogroup", { name: "Provisioning" });
    expect(within(group).getAllByRole("radio")).toHaveLength(3);
    expect(mode("Invitation only")).toBeChecked();
    expect(
      within(group).getByText(
        "Creates an account the first time someone without one signs in.",
      ),
    ).toBeVisible();
    expect(
      within(group).getByText(
        "Only linked accounts can sign in. New accounts need an invitation.",
      ),
    ).toBeVisible();
    expect(
      within(group).getByText(
        "Links to existing accounts only. Never creates one, not even from an invitation.",
      ),
    ).toBeVisible();
  });

  it("says VRChat is a weaker proof as its own sentence, and only for VRChat", () => {
    const { unmount } = mount(providerFixture({ protocol: "vrchat" }));
    const warning = screen.getByText(
      /VRChat proves a profile through a link in its bio/,
    );
    expect(warning).toBeVisible();
    expect(
      screen.getByText(/this provider can't be used to sign in/),
    ).toBeVisible();
    // The two sentences are separate blocks, not one glued run of text.
    expect(warning.textContent).not.toMatch(/linked to it\.When/);
    expect(warning.textContent).not.toContain("can't be used to sign in");
    unmount();

    mount(providerFixture({ protocol: "oidc" }));
    expect(
      screen.queryByText(/VRChat proves a profile through a link in its bio/),
    ).toBeNull();
    expect(
      screen.getByText(/this provider can't be used to sign in/),
    ).toBeVisible();
  });

  it("saves a changed provisioning mode with no confirmation", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ mode: "invite_only", config: publicOidcConfig }));

    await user.click(mode("Links existing accounts"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Corporate SSO",
      mode: "link_only",
      allowSignIn: true,
      config: storedConfig,
    });
  });
});

describe("with sign-in off", () => {
  it("disables creating accounts and says why, in text", () => {
    mount(providerFixture({ allowSignIn: false }));

    expect(mode("Creates accounts")).toHaveAttribute("aria-disabled", "true");
    const reason = screen.getByText("Turn sign-in on to create accounts.");
    expect(reason).toBeVisible();
    // Outside the dimmed card, so it keeps its own contrast, and tied to the
    // group as its description.
    expect(reason.closest('[aria-disabled="true"]')).toBeNull();
    expect(reason.closest("label")).toBeNull();
    expect(
      screen.getByRole("radiogroup", { name: "Provisioning" }),
    ).toHaveAccessibleDescription("Turn sign-in on to create accounts.");
    expect(mode("Invitation only")).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("gives no such reason while sign-in is on", () => {
    mount(providerFixture());
    expect(mode("Creates accounts")).not.toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(
      screen.queryByText("Turn sign-in on to create accounts."),
    ).toBeNull();
  });

  it("resets a chosen creates-accounts mode to invitation-only when sign-in turns off", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ mode: "auto_provision" }));
    expect(mode("Creates accounts")).toBeChecked();

    await user.click(signInSwitch());

    await waitFor(() => expect(mode("Invitation only")).toBeChecked());
    expect(mode("Creates accounts")).toHaveAttribute("aria-disabled", "true");
  });
});

describe("turning sign-in off", () => {
  it("states the consequence and names the action in the footer", async () => {
    const user = userEvent.setup();
    mount(providerFixture());

    await user.click(signInSwitch());

    expect(screen.getByText(lockout)).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Turn off sign-in and save" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says in the footer that creating accounts falls back to invitations", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ mode: "auto_provision" }));

    await user.click(signInSwitch());

    expect(
      screen.getByText(
        `${lockout} Provisioning also changes from Creates accounts to Invitation only, because creating accounts needs sign-in.`,
      ),
    ).toBeVisible();
  });

  it("does not mention provisioning when it was not creating accounts", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ mode: "invite_only" }));

    await user.click(signInSwitch());

    expect(screen.queryByText(/Provisioning also changes/)).toBeNull();
  });

  it("asks before sending, and a cancelled dialog sends nothing", async () => {
    const user = userEvent.setup();
    mount(providerFixture());

    await user.click(signInSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn off sign-in and save" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Turn off sign-in for Corporate SSO?",
    });
    // The footer said what it costs; the dialog only asks.
    expect(within(dialog).queryByText(lockout)).toBeNull();
    expect(
      within(dialog).getByText(/You can turn it back on at any time/),
    ).toBeVisible();
    expect(puts()).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(puts()).toHaveLength(0);
    // The reader's choice stays in the form, unsent.
    expect(signInSwitch()).not.toBeChecked();
  });

  it("saves with sign-in off and invitation-only after the dialog is confirmed", async () => {
    const user = userEvent.setup();
    mount(
      providerFixture({ mode: "auto_provision", config: publicOidcConfig }),
    );

    await user.click(signInSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn off sign-in and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Turn off sign-in" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      displayName: "Corporate SSO",
      mode: "invite_only",
      allowSignIn: false,
      config: storedConfig,
    });
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("keeps the dialog open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(providerFixture(), (request) =>
      request.method === "PUT"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(signInSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn off sign-in and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Turn off sign-in" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(Response.json(providerFixture({ allowSignIn: false })));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("reports a refused confirmed save once, on the section", async () => {
    const user = userEvent.setup();
    mount(providerFixture(), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(signInSwitch());
    await user.click(
      screen.getByRole("button", { name: "Turn off sign-in and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Turn off sign-in" }),
    );

    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("a plain save", () => {
  it("reports a refused save once, on the section", async () => {
    const user = userEvent.setup();
    mount(providerFixture({ mode: "invite_only" }), (request) =>
      request.method === "PUT"
        ? Response.json({ code: "internal_error" }, { status: 500 })
        : undefined,
    );

    await user.click(mode("Links existing accounts"));
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
