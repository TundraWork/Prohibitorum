import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";
import { type Provider, providerFixture } from "@/test/identity-provider";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";

function open(provider: Provider) {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/identity-providers`]: () =>
      Response.json({ items: [provider], nextCursor: "" }),
    [`GET ${base}/identity-providers/${provider.slug}`]: () =>
      Response.json(provider),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountPage(provider: Provider) {
  const api = open(provider);
  renderApp(`/admin/identity-providers/${provider.slug}`, queryClient);
  await screen.findByRole("heading", { level: 1, name: provider.displayName });
  return api;
}

const steam = (overrides: Partial<Provider> = {}) =>
  providerFixture({
    slug: "steam-main",
    displayName: "Steam login",
    protocol: "steam",
    secretConfigured: true,
    ...overrides,
  });

const vrchat = (overrides: Partial<Provider> = {}) =>
  providerFixture({
    slug: "vrc",
    displayName: "VRChat login",
    protocol: "vrchat",
    supportsOperator: true,
    allowSignIn: false,
    mode: "link_only",
    ...overrides,
  });

async function railLinks() {
  const rail = await screen.findByRole("navigation", { name: "On this page" });
  return within(rail)
    .getAllByRole("link")
    .map((link) => link.textContent);
}

describe("the provider header", () => {
  it("names the provider, with its slug and protocol", async () => {
    await mountPage(providerFixture());

    const heading = screen.getByRole("heading", { level: 1 });
    expect(heading).toHaveTextContent("Corporate SSO");
    expect(
      screen.queryByRole("heading", { level: 1, name: "Federation" }),
    ).toBeNull();
    const header = heading.closest(
      '[data-slot="object-header"]',
    ) as HTMLElement;
    expect(within(header).getByText("corp")).toBeVisible();
    expect(within(header).getByText("OpenID Connect")).toBeVisible();
    expect(screen.getByRole("button", { name: "Copy the slug" })).toBeVisible();
  });

  it("copies the slug", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    await mountPage(providerFixture());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await user.click(screen.getByRole("button", { name: "Copy the slug" }));
    expect(writeText).toHaveBeenCalledWith("corp");
  });

  it.each([
    ["Steam", steam(), "Steam"],
    ["VRChat", vrchat(), "VRChat"],
  ])("names a %s provider's protocol", async (_name, provider, label) => {
    await mountPage(provider);
    const header = screen
      .getByRole("heading", { level: 1 })
      .closest('[data-slot="object-header"]') as HTMLElement;
    expect(within(header).getByText(label)).toBeVisible();
  });

  it("says enabled for a provider in use", async () => {
    await mountPage(providerFixture());
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled"]);
  });

  it("says when a provider only verifies and is not ready, with the reason as text", async () => {
    await mountPage(
      vrchat({
        disabled: true,
        ready: false,
        secretConfigured: false,
        secretStatus: "unconfigured",
      }),
    );
    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Disabled", "Sign-in off", "Not ready"]);
    // Visible text under the header, not a tooltip.
    const note = document.querySelector('[data-slot="provider-not-ready"]');
    expect(note).toHaveTextContent(
      "Sign in the operator account before enabling it.",
    );
    expect(note).toBeVisible();
  });

  it("words the reason for what is failing when the provider is enabled", async () => {
    await mountPage(vrchat({ ready: false, secretStatus: "invalid" }));
    expect(
      document.querySelector('[data-slot="provider-not-ready"]'),
    ).toHaveTextContent(
      "Linking and verifying through this provider fail until the operator account is signed in again.",
    );
  });

  it("gives no reason for a ready provider", async () => {
    await mountPage(providerFixture());
    expect(screen.queryByText(/before it can be enabled/)).toBeNull();
  });
});

describe("the page outline", () => {
  it("lists an OIDC provider's sections in order, the danger one last", async () => {
    await mountPage(providerFixture());
    await waitFor(async () =>
      expect(await railLinks()).toEqual([
        "Sign-in",
        "Connection",
        "Accounts and claims",
        "Diagnostics",
        "Appearance",
        "Disable or delete",
      ]),
    );
  });

  it("lists a Steam provider's sections", async () => {
    await mountPage(steam());
    await waitFor(async () =>
      expect(await railLinks()).toEqual([
        "Sign-in",
        "Credentials",
        "Appearance",
        "Disable or delete",
      ]),
    );
  });

  it("lists a VRChat provider's sections", async () => {
    await mountPage(vrchat());
    await waitFor(async () =>
      expect(await railLinks()).toEqual([
        "Sign-in",
        "Operator session",
        "Appearance",
        "Disable or delete",
      ]),
    );
  });
});

describe("the secret", () => {
  it("is set from the Steam provider's own section", async () => {
    const user = userEvent.setup();
    const api = await mountPage(steam({ secretConfigured: false }));
    api.routes[`POST ${base}/identity-providers/rotate-secret`] = () =>
      new Response(null, { status: 204 });

    const section = screen
      .getByRole("heading", { level: 2, name: "Credentials" })
      .closest("section") as HTMLElement;
    await user.click(within(section).getByRole("button", { name: "Set" }));
    const dialog = await screen.findByRole("dialog", {
      name: "Set the Web API key",
    });
    await user.type(within(dialog).getByLabelText("Key"), "KEY");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(
        api.sent("POST", `${base}/identity-providers/rotate-secret`),
      ).toHaveLength(1),
    );
    expect(
      api.sent("POST", `${base}/identity-providers/rotate-secret`)[0]?.body,
    ).toEqual({ slug: "steam-main", secret: "KEY" });
  });

  it("is not in the danger zone", async () => {
    await mountPage(steam());
    const danger = screen
      .getByRole("heading", { level: 2, name: "Disable or delete" })
      .closest("section") as HTMLElement;
    expect(within(danger).queryByText(/key|secret/i)).toBeNull();
  });
});
