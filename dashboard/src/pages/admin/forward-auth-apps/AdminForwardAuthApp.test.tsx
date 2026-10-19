import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { fakeApi, publicApi, renderApp, testSession } from "@/test/app";
import {
  type ForwardAuthApp,
  forwardAuthAppFixture,
} from "@/test/forward-auth-app";

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

function open(app: ForwardAuthApp, role: "admin" | "user" = "admin") {
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/saml-applications`]: () =>
      Response.json({ items: [], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps`]: () =>
      Response.json({ items: [app], nextCursor: "" }),
    [`GET ${base}/forward-auth-apps/${app.clientId}`]: () => Response.json(app),
    [`GET ${base}/managed-applications/forward_auth/${app.clientId}/access`]:
      () =>
        Response.json({
          app: {
            kind: "forward_auth",
            appId: app.clientId,
            displayName: app.displayName,
            accessRestricted: app.accessRestricted,
          },
          accessRestricted: app.accessRestricted,
          providers: [],
          groups: [],
        }),
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/forward-auth-apps/${app.clientId}/managers`]: () =>
      Response.json([]),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
  });
}

async function mountPage(
  app: ForwardAuthApp,
  role: "admin" | "user" = "admin",
) {
  const api = open(app, role);
  renderApp(`/admin/forward-auth-apps/${app.clientId}`, queryClient);
  await screen.findByRole("heading", { level: 1, name: app.displayName });
  return api;
}

describe("the application header", () => {
  it("names the application with its state, Client ID and hostname", async () => {
    await mountPage(forwardAuthAppFixture());

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Enabled", "Open"]);
    const header = screen.getByRole("heading", { level: 1 })
      .parentElement as HTMLElement;
    expect(within(header).getByText("grafana")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Copy the client ID" }),
    ).toBeVisible();
    expect(within(header).getByText("grafana.example.test")).toBeVisible();
  });

  it("says disabled and restricted when the application is both", async () => {
    await mountPage(
      forwardAuthAppFixture({ disabled: true, accessRestricted: true }),
    );

    const statuses = screen.getByRole("list", { name: "Status" });
    expect(
      within(statuses)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Disabled", "Restricted"]);
  });

  it("leaves the hostname out when the record has none", async () => {
    await mountPage(forwardAuthAppFixture({ forwardAuthHost: "" }));
    const header = screen.getByRole("heading", { level: 1 })
      .parentElement as HTMLElement;
    expect(within(header).queryByText("grafana.example.test")).toBeNull();
  });

  it("copies the Client ID from the header", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    await mountPage(forwardAuthAppFixture());
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await user.click(
      screen.getByRole("button", { name: "Copy the client ID" }),
    );
    expect(writeText).toHaveBeenCalledWith("grafana");
  });
});

describe("the page outline", () => {
  it("lists the sections in order with the danger one last", async () => {
    await mountPage(forwardAuthAppFixture());

    const rail = await screen.findByRole("navigation", {
      name: "On this page",
    });
    await waitFor(() =>
      expect(
        within(rail)
          .getAllByRole("link")
          .map((link) => link.textContent),
      ).toEqual([
        "Service",
        "Access",
        "Managers",
        "Headers sent to the service",
        "Traefik configuration",
        "Appearance",
        "Disable or delete",
      ]),
    );
  });

  it("leaves Managers out for a delegated manager and says they manage it", async () => {
    await mountPage(forwardAuthAppFixture(), "user");

    expect(screen.getByText("You manage this application.")).toBeVisible();
    const rail = await screen.findByRole("navigation", {
      name: "On this page",
    });
    await waitFor(() =>
      expect(within(rail).getAllByRole("link").length).toBeGreaterThan(3),
    );
    expect(within(rail).queryByRole("link", { name: "Managers" })).toBeNull();
    expect(
      within(rail).getByRole("link", { name: "Traefik configuration" }),
    ).toBeVisible();
  });
});
