import type { QueryClient } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";

/** The origin jsdom reports, which the preview draws its addresses on. */
const origin = window.location.origin;

let queryClient: QueryClient;

/**
 * Mounts the real new-provider page at its route, with the reads the
 * protected layout makes on the way in, so the preview is exercised in the
 * form it is part of: the identifier field, the protocol radios and the copy
 * fields under them.
 */
function mountPage() {
  fakeApi(publicApi({ ...testSession, role: "admin" }));
  renderApp("/admin/identity-providers/new", queryClient);
}

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

describe("the new-provider callback preview", () => {
  it("draws nothing until an identifier is typed", async () => {
    mountPage();
    const identifier = await screen.findByLabelText("Identifier");
    expect(
      screen.queryByRole("group", { name: "Callback addresses" }),
    ).not.toBeInTheDocument();

    await userEvent.setup().type(identifier, "c");
    expect(
      screen.getByRole("group", { name: "Callback addresses" }),
    ).toBeInTheDocument();
  });

  it("draws nothing for an identifier the server would refuse", async () => {
    mountPage();
    const user = userEvent.setup();
    const identifier = await screen.findByLabelText("Identifier");
    await user.type(identifier, "Corp SSO");
    expect(
      screen.queryByRole("group", { name: "Callback addresses" }),
    ).not.toBeInTheDocument();

    await user.clear(identifier);
    await user.type(identifier, "corp-sso");
    expect(
      screen.getByRole("group", { name: "Callback addresses" }),
    ).toBeInTheDocument();
  });

  it("previews the three addresses an OIDC identifier produces", async () => {
    mountPage();
    // A leading space is trimmed, the way the submit sends the identifier.
    await userEvent
      .setup()
      .type(await screen.findByLabelText("Identifier"), " corp");
    const preview = screen.getByRole("group", {
      name: "Callback addresses",
    });

    const login = within(preview).getByRole("textbox", { name: "Sign-in" });
    expect(login).toHaveValue(
      `${origin}/api/prohibitorum/auth/federation/corp/callback`,
    );
    const link = within(preview).getByRole("textbox", {
      name: "Link an account",
    });
    expect(link).toHaveValue(
      `${origin}/api/prohibitorum/me/identities/link/corp/callback`,
    );
    const test = within(preview).getByRole("textbox", { name: "Test" });
    expect(test).toHaveValue(
      `${origin}/api/prohibitorum/auth/federation/corp/test/callback`,
    );
  });

  it("drops the test address when the protocol is not OIDC", async () => {
    mountPage();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("radio", { name: "Steam" }));
    await user.type(await screen.findByLabelText("Identifier"), "steam");

    const preview = screen.getByRole("group", {
      name: "Callback addresses",
    });
    expect(
      within(preview).getByRole("textbox", { name: "Sign-in" }),
    ).toHaveValue(`${origin}/api/prohibitorum/auth/federation/steam/callback`);
    expect(
      within(preview).queryByRole("textbox", { name: "Test" }),
    ).not.toBeInTheDocument();
  });

  it("draws no preview for a VRChat provider", async () => {
    mountPage();
    const user = userEvent.setup();
    await user.click(await screen.findByRole("radio", { name: "VRChat" }));
    await user.type(await screen.findByLabelText("Identifier"), "vrchat");

    expect(
      screen.queryByRole("group", { name: "Callback addresses" }),
    ).not.toBeInTheDocument();
  });
});
