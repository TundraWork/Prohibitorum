import type { QueryClient } from "@tanstack/react-query";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { type Provider, providerFixture } from "@/test/identity-provider";

const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const base = "/api/prohibitorum";

function serve(list: () => Response | Promise<Response>) {
  return fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/identity-providers`]: list,
    [`GET ${base}/identity-providers/corp`]: () =>
      Response.json(providerFixture()),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

function page(items: Provider[]) {
  return () => Response.json({ items, nextCursor: "" });
}

async function mountList(items: Provider[]): Promise<HTMLElement> {
  serve(page(items));
  renderApp("/admin/identity-providers", queryClient);
  await screen.findByRole("heading", { level: 1, name: "Identity providers" });
  return await screen.findByRole("table", { name: "Identity providers" });
}

function rowOf(table: HTMLElement, name: string): HTMLElement {
  const row = within(table).getByRole("link", { name }).closest("tr");
  if (row === null) throw new Error(`no row for ${name}`);
  return row;
}

// The list carries the short form; the page says the whole sentence.
const oidcReason = "Needs a valid connection";
const oidcDisabledReason = "Needs a valid connection before it can be enabled.";

describe("the page", () => {
  it("heads the list with what it is for and offers Add a provider", async () => {
    await mountList([providerFixture()]);

    expect(
      document.querySelector('[data-slot="page-description"]'),
    ).not.toBeNull();
    expect(
      screen.getByRole("link", { name: "Add a provider" }),
    ).toHaveAttribute("href", "/admin/identity-providers/new");
  });

  it("says there are no providers, in one line of help, and offers the create action once", async () => {
    await mountList([]);

    expect(await screen.findByText("No providers yet")).toBeVisible();
    expect(
      screen.getByText(/Add a provider so people can sign in/),
    ).toBeVisible();
    expect(
      screen.getAllByRole("link", { name: "Add a provider" }),
    ).toHaveLength(1);
  });
});

describe("a row", () => {
  it("names the provider and its slug, its protocol with the provisioning mode under it, and how many accounts use it", async () => {
    const table = await mountList([
      providerFixture({ mode: "auto_provision", linkedAccountCount: 1234 }),
    ]);

    const row = rowOf(table, "Corporate SSO");
    expect(within(row).getByText("corp")).toBeVisible();
    expect(within(row).getByText("OpenID Connect")).toBeVisible();
    expect(within(row).getByText("Creates accounts")).toBeVisible();
    expect(within(row).getByText("1234")).toBeVisible();
  });

  it("shows a count of nothing as 0 and keeps an unreadable protocol out of the cell", async () => {
    const table = await mountList([
      providerFixture({ linkedAccountCount: 0, protocol: "unheard-of" }),
    ]);

    const row = rowOf(table, "Corporate SSO");
    expect(within(row).getByText("0")).toBeVisible();
    expect(within(row).queryByText("unheard-of")).toBeNull();
  });

  it("says in words that a disabled provider is disabled, not only by dimming it", async () => {
    const table = await mountList([
      providerFixture({ disabled: true }),
      providerFixture({ slug: "steam", displayName: "Steam login" }),
    ]);

    expect(
      within(rowOf(table, "Corporate SSO")).getByText("Disabled"),
    ).toBeVisible();
    expect(
      within(rowOf(table, "Steam login")).queryByText("Disabled"),
    ).toBeNull();
  });

  it("marks a provider that cannot sign anyone in", async () => {
    const table = await mountList([
      providerFixture({ allowSignIn: false }),
      providerFixture({ slug: "steam", displayName: "Steam login" }),
    ]);

    expect(
      within(rowOf(table, "Corporate SSO")).getByText("Sign-in off"),
    ).toBeVisible();
    expect(
      within(rowOf(table, "Steam login")).queryByText("Sign-in off"),
    ).toBeNull();
  });

  it("says a provider is not ready and why, in text a reader can see", async () => {
    const table = await mountList([
      providerFixture({ ready: false }),
      providerFixture({
        slug: "steam",
        displayName: "Steam login",
        protocol: "steam",
        ready: false,
        disabled: true,
      }),
      providerFixture({ slug: "ok", displayName: "Fine" }),
    ]);

    const oidc = rowOf(table, "Corporate SSO");
    expect(within(oidc).getByText("Not ready")).toBeVisible();
    expect(within(oidc).getByText(oidcReason)).toBeVisible();

    // A disabled provider is told what stands between it and being enabled.
    const steam = rowOf(table, "Steam login");
    expect(within(steam).getByText("Needs a Web API key")).toBeVisible();
    expect(
      within(steam).queryByText("Set the Web API key before enabling it."),
    ).toBeNull();

    const fine = rowOf(table, "Fine");
    expect(within(fine).queryByText("Not ready")).toBeNull();
    expect(within(fine).queryByText(oidcReason)).toBeNull();
    expect(within(fine).queryByText(oidcDisabledReason)).toBeNull();
  });

  it("opens the provider from the row action", async () => {
    const user = userEvent.setup();
    serve(page([providerFixture()]));
    const router = renderApp("/admin/identity-providers", queryClient);
    const table = await screen.findByRole("table", {
      name: "Identity providers",
    });

    await user.click(
      await within(table).findByRole("button", { name: "Open Corporate SSO" }),
    );

    await screen.findByRole("heading", { level: 1, name: "Corporate SSO" });
    expect(router.state.location.pathname).toBe(
      "/admin/identity-providers/corp",
    );
  });
});

describe("a failed read", () => {
  it("is reported once, on the page, and the empty state does not contradict it", async () => {
    serve(() => apiError("internal_error", 500));
    renderApp("/admin/identity-providers", queryClient);

    await screen.findByRole("alert");
    expect(notifyError).not.toHaveBeenCalled();
    expect(screen.queryByText("No providers yet")).toBeNull();
  });
});
