import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";
import { type OidcApp, oidcAppFixture } from "@/test/oidc-app";

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

function open(
  app: OidcApp,
  role: "admin" | "user" = "admin",
  extra: Record<string, () => Response> = {},
) {
  const apps = Response.json({ items: [app], nextCursor: "" });
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: () => apps.clone(),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/oidc-applications/${app.clientId}`]: () => Response.json(app),
    [`GET ${base}/managed-applications/oidc/${app.clientId}/access`]: () =>
      Response.json({
        app: {
          kind: "oidc",
          appId: app.clientId,
          displayName: app.displayName,
          accessRestricted: app.accessRestricted,
        },
        accessRestricted: app.accessRestricted,
        providers: [],
        groups: [],
      }),
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/oidc-applications/${app.clientId}/managers`]: () =>
      Response.json([]),
    "GET /.well-known/openid-configuration": () =>
      Response.json({ issuer: "https://id.configured.example.test" }),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
    ...extra,
  });
}

async function mountPage(
  app: OidcApp,
  role: "admin" | "user" = "admin",
  extra: Record<string, () => Response> = {},
) {
  const api = open(app, role, extra);
  renderApp(`/admin/oidc-applications/${app.clientId}`, queryClient);
  await screen.findByRole("heading", { level: 1, name: app.displayName });
  return api;
}

describe("the application header", () => {
  it("names the application with its state, client ID and kind", async () => {
    await mountPage(oidcAppFixture());

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled", "Open"]);
    expect(screen.getByText("grafana")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Copy the client ID" }),
    ).toBeVisible();
    expect(screen.getByText("Confidential")).toBeVisible();
  });

  it("says disabled and restricted when the application is both", async () => {
    await mountPage(oidcAppFixture({ disabled: true, accessRestricted: true }));

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Disabled", "Restricted"]);
  });

  it("says a client without a secret is public", async () => {
    await mountPage(oidcAppFixture({ clientAuthMethod: "none" }));
    expect(screen.getByText("Public")).toBeVisible();
  });

  it("opens the launch address from a button named for the application", async () => {
    await mountPage(oidcAppFixture());

    const link = screen.getByRole("link", { name: "Open Grafana" });
    expect(link).toHaveAttribute("href", "https://grafana.example.test");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("offers no launch button when there is no launch address", async () => {
    const { launchUrl: _none, ...withoutLaunch } = oidcAppFixture();
    await mountPage(withoutLaunch);
    expect(screen.queryByRole("link", { name: /^Open / })).toBeNull();
  });

  it("copies the configured issuer from the outline, not the page origin", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    // After setup(), which installs its own clipboard stub.
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await mountPage(oidcAppFixture());

    const rail = (
      await screen.findByRole("navigation", { name: "On this page" })
    ).closest("aside") as HTMLElement;
    expect(
      await within(rail).findByText("https://id.configured.example.test"),
    ).toBeVisible();
    await user.click(
      within(rail).getByRole("button", { name: "Copy the issuer" }),
    );

    expect(writeText).toHaveBeenCalledWith(
      "https://id.configured.example.test",
    );
  });

  it("leaves the issuer out when the discovery document cannot be read", async () => {
    const api = await mountPage(oidcAppFixture(), "admin", {
      "GET /.well-known/openid-configuration": () =>
        new Response(null, { status: 500 }),
    });

    const rail = (
      await screen.findByRole("navigation", { name: "On this page" })
    ).closest("aside") as HTMLElement;
    await waitFor(() =>
      expect(api.sent("GET", "/.well-known/openid-configuration")).toHaveLength(
        1,
      ),
    );
    expect(within(rail).queryByText("Issuer")).toBeNull();
    expect(
      within(rail).queryByRole("button", { name: "Copy the issuer" }),
    ).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("the page outline", () => {
  it("lists the sections in order with the danger one last", async () => {
    await mountPage(oidcAppFixture());

    const rail = await screen.findByRole("navigation", {
      name: "On this page",
    });
    await waitFor(() =>
      expect(
        within(rail)
          .getAllByRole("link")
          .map((link) => link.textContent),
      ).toEqual([
        "Sign-in",
        "Access",
        "Managers",
        "Identity",
        "Appearance",
        "Credentials",
        "Disable or delete",
      ]),
    );
  });

  it("leaves Managers out for a delegated manager and says they manage it", async () => {
    await mountPage(oidcAppFixture(), "user");

    expect(screen.getByText("You manage this application.")).toBeVisible();
    const rail = await screen.findByRole("navigation", {
      name: "On this page",
    });
    await waitFor(() =>
      expect(within(rail).getAllByRole("link").length).toBeGreaterThan(3),
    );
    expect(within(rail).queryByRole("link", { name: "Managers" })).toBeNull();
    expect(
      within(rail).getByRole("link", { name: "Credentials" }),
    ).toBeVisible();
  });
});

describe("the credentials of a public client", () => {
  it("shows a line instead of the rotation", async () => {
    await mountPage(oidcAppFixture({ clientAuthMethod: "none" }));

    expect(
      await screen.findByText(/A public client has no secret/),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Rotate secret" })).toBeNull();
  });
});

describe("rotating the secret", () => {
  it("reveals the new secret once it is confirmed", async () => {
    const user = userEvent.setup();
    await mountPage(oidcAppFixture(), "admin", {
      [`POST ${base}/oidc-applications/rotate-secret`]: () =>
        Response.json({ clientId: "grafana", secret: "rotated-secret-value" }),
    });

    await user.click(
      await screen.findByRole("button", { name: "Rotate secret" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Rotate secret" }),
    );

    expect(await screen.findByText("rotated-secret-value")).toBeVisible();
    // The reader is already on the application, so nothing is "gone to".
    expect(
      screen.queryByRole("button", { name: "Go to the application" }),
    ).toBeNull();
    expect(screen.getByRole("button", { name: "Done" })).toBeVisible();
  });
});
