import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";
import { type SamlApp, samlAppFixture } from "@/test/saml-app";

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

function open(app: SamlApp, role: "admin" | "user" = "admin") {
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [app], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications/${app.id}`]: () => Response.json(app),
    [`GET ${base}/managed-applications/saml/${app.id}/access`]: () =>
      Response.json({
        app: {
          kind: "saml",
          appId: String(app.id),
          displayName: app.displayName,
          accessRestricted: app.accessRestricted,
        },
        accessRestricted: app.accessRestricted,
        providers: [],
        groups: [],
      }),
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/saml-applications/${app.id}/managers`]: () =>
      Response.json([]),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountPage(app: SamlApp, role: "admin" | "user" = "admin") {
  const api = open(app, role);
  renderApp(`/admin/saml-applications/${app.id}`, queryClient);
  await screen.findByRole("heading", { level: 1, name: app.displayName });
  return api;
}

describe("the application header", () => {
  it("names the application with its state and Entity ID", async () => {
    await mountPage(samlAppFixture());

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled", "Open"]);
    expect(
      within(
        screen.getByRole("heading", { level: 1 }).parentElement as HTMLElement,
      ).getByText("https://wiki.example.test/saml/metadata"),
    ).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Copy the Entity ID" }),
    ).toBeVisible();
  });

  it("says disabled and restricted when the application is both", async () => {
    await mountPage(samlAppFixture({ disabled: true, accessRestricted: true }));

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Disabled", "Restricted"]);
  });

  it("says when the signing certificate expires", async () => {
    await mountPage(samlAppFixture());
    expect(screen.getByText("Certificate expires Mar 15, 2031")).toBeVisible();
  });

  it("says when the signing certificate has expired", async () => {
    await mountPage(
      samlAppFixture({
        keys: [{ use: "signing", notAfter: "2020-01-01T12:00:00Z" }],
      }),
    );
    expect(screen.getByText("Certificate expired Jan 1, 2020")).toBeVisible();
  });

  it("says when there is no signing certificate", async () => {
    await mountPage(samlAppFixture({ keys: [] }));
    expect(screen.getByText("No signing certificate")).toBeVisible();
  });
});

describe("the page outline", () => {
  it("lists the sections in order with the danger one last", async () => {
    await mountPage(samlAppFixture());

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
        "Identity projection",
        "Metadata",
        "Appearance",
        "Disable or delete",
      ]),
    );
  });

  it("leaves Managers out for a delegated manager and says they manage it", async () => {
    await mountPage(samlAppFixture(), "user");

    expect(
      screen.getByText("You are assigned to manage this application."),
    ).toBeVisible();
    const rail = await screen.findByRole("navigation", {
      name: "On this page",
    });
    await waitFor(() =>
      expect(within(rail).getAllByRole("link").length).toBeGreaterThan(3),
    );
    expect(within(rail).queryByRole("link", { name: "Managers" })).toBeNull();
    expect(within(rail).getByRole("link", { name: "Metadata" })).toBeVisible();
  });

  it("copies the Entity ID from the header", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    await mountPage(samlAppFixture());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await user.click(
      screen.getByRole("button", { name: "Copy the Entity ID" }),
    );
    expect(writeText).toHaveBeenCalledWith(
      "https://wiki.example.test/saml/metadata",
    );
  });
});
