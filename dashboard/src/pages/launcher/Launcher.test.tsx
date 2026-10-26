import type { MessageDescriptor } from "@lingui/core";
import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testConfig,
  testSession,
} from "@/test/app";

type App = components["schemas"]["LaunchpadApp"];
type Grant = components["schemas"]["ConsentedApp"];

const grafana: App = {
  kind: "forward_auth",
  id: "grafana",
  name: "Grafana",
  launchUrl: "https://grafana.example.test/",
};
const gitea: App = {
  kind: "oidc",
  id: "gitea",
  name: "Gitea",
  launchUrl: "https://git.example.test/",
  requireConsent: true,
};
const docs: App = {
  kind: "oidc",
  id: "docs",
  name: "Docs",
  launchUrl: "https://docs.example.test/",
  requireConsent: false,
};
const wiki: App = {
  kind: "oidc",
  id: "wiki",
  name: "Wiki",
  launchUrl: "https://wiki.example.test/",
  requireConsent: true,
};
const expenses: App = {
  kind: "saml",
  id: "7",
  name: "Expenses",
  launchUrl: "/saml/sso/init?sp=https%3A%2F%2Fexpenses.example.test",
};

const grant = (kind: "oidc" | "saml", clientId: string): Grant => ({
  kind,
  clientId,
  name: clientId,
  grantedAt: new Date().toISOString(),
  scopes: [],
});

const notifyError = vi.fn<(error: unknown) => void>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  notifyError.mockReset();
  notifySuccess.mockReset();
  queryClient = createQueryClient(notifyError, notifySuccess);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function mount({
  apps,
  grants,
  role = "user",
  routes = {},
}: {
  apps: App[];
  grants: Grant[];
  role?: "user" | "admin";
  routes?: FakeApi["routes"];
}): FakeApi {
  let current = grants;
  const api = fakeApi({
    ...publicApi({ ...testSession, role }),
    "GET /api/prohibitorum/me/apps": () => Response.json(apps),
    "GET /api/prohibitorum/me/consent": () => Response.json(current),
    "POST /api/prohibitorum/me/consent/revoke": async (request) => {
      const body = (await request.json()) as { clientId: string };
      current = current.filter((entry) => entry.clientId !== body.clientId);
      return new Response(null, { status: 204 });
    },
    ...routes,
  });
  renderApp("/", queryClient);
  return api;
}

const pad = () => screen.findByRole("list", { name: "Your applications" });
const tileNames = async () =>
  within(await pad())
    .getAllByRole("link")
    .map((link) => link.textContent);

describe("the launcher", () => {
  it("puts connected applications on the pad and the rest behind Add app", async () => {
    mount({
      apps: [docs, expenses, gitea, grafana, wiki],
      grants: [grant("oidc", "gitea"), grant("saml", "7")],
    });

    expect(await tileNames()).toEqual([
      "Open Docs in a new tab",
      "Open Expenses in a new tab",
      "Open Gitea in a new tab",
      "Open Grafana in a new tab",
    ]);

    await userEvent.click(screen.getByRole("button", { name: /Add app/ }));
    const dialog = await screen.findByRole("dialog", {
      name: "Connect an app",
    });
    expect(
      within(dialog).getByRole("link", { name: "Open Wiki in a new tab" }),
    ).toBeInTheDocument();
    expect(within(dialog).queryByText("Gitea")).not.toBeInTheDocument();
  });

  it("opens an application in a new tab from its tile", async () => {
    mount({ apps: [grafana], grants: [] });
    const link = await screen.findByRole("link", {
      name: "Open Grafana in a new tab",
    });
    expect(link).toHaveAttribute("href", grafana.launchUrl);
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("says how each application signs the member in", async () => {
    mount({
      apps: [expenses, gitea, grafana],
      grants: [grant("oidc", "gitea"), grant("saml", "7")],
    });
    await pad();
    expect(
      screen.getByRole("button", {
        name: "OIDC — OpenID Connect sign-in",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "SAML — SAML sign-in" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: "Forward auth — Requires sign-in to access",
      }),
    ).toBeInTheDocument();
  });

  it("marks access as granted, and offers its removal, only where there is a grant to remove", async () => {
    mount({
      apps: [docs, gitea, grafana],
      // An old grant on a consent-free application changes nothing.
      grants: [grant("oidc", "gitea"), grant("oidc", "docs")],
    });
    const list = await pad();
    const tile = (name: string) =>
      within(list)
        .getByRole("link", { name: `Open ${name} in a new tab` })
        .closest("li") as HTMLElement;
    expect(
      within(tile("Gitea")).getByText("Access granted"),
    ).toBeInTheDocument();
    expect(within(tile("Docs")).queryByText("Access granted")).toBeNull();
    expect(within(tile("Grafana")).queryByText("Access granted")).toBeNull();

    for (const name of ["Docs", "Grafana"]) {
      await userEvent.click(
        screen.getByRole("button", { name: `More for ${name}` }),
      );
      const menu = await screen.findByRole("menu");
      expect(
        within(menu).getByRole("menuitem", { name: "Copy link" }),
      ).toBeInTheDocument();
      expect(
        within(menu).queryByRole("menuitem", { name: "Remove access" }),
      ).toBeNull();
      await userEvent.keyboard("{Escape}");
      await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    }
  });

  it("removes a grant with its kind, and the application moves behind Add app", async () => {
    const api = mount({
      apps: [expenses, grafana],
      grants: [grant("saml", "7")],
    });
    await pad();
    await userEvent.click(
      screen.getByRole("button", { name: "More for Expenses" }),
    );
    await userEvent.click(
      await screen.findByRole("menuitem", { name: "Remove access" }),
    );
    const confirm = await screen.findByRole("alertdialog");
    await userEvent.click(
      within(confirm).getByRole("button", { name: "Remove access" }),
    );

    await waitFor(() =>
      expect(
        api
          .sent("POST", "/api/prohibitorum/me/consent/revoke")
          .map((request) => request.body),
      ).toEqual([{ kind: "saml", clientId: "7" }]),
    );
    await waitFor(async () =>
      expect(await tileNames()).toEqual(["Open Grafana in a new tab"]),
    );
    expect(screen.getByRole("button", { name: /Add app/ })).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("offers an administrator the application's settings, and a member not", async () => {
    mount({ apps: [grafana], grants: [], role: "admin" });
    await pad();
    await userEvent.click(
      screen.getByRole("button", { name: "More for Grafana" }),
    );
    expect(
      await screen.findByRole("menuitem", { name: "Manage application" }),
    ).toHaveAttribute("href", "/admin/forward-auth-apps/grafana");
  });

  it("sends an administrator's account menu to the first management page", async () => {
    mount({ apps: [grafana], grants: [], role: "admin" });
    await pad();
    await userEvent.click(screen.getByRole("button", { name: "Your account" }));
    expect(
      await screen.findByRole("menuitem", { name: "Administration" }),
    ).toHaveAttribute("href", "/admin/users");
  });

  it("leaves the maintenance banner to the console", async () => {
    mount({
      apps: [grafana],
      grants: [],
      role: "admin",
      routes: {
        "GET /api/prohibitorum/config": () =>
          Response.json({ ...testConfig, maintenanceMode: true }),
      },
    });
    await pad();
    expect(
      screen.queryByRole("status", { name: "Maintenance mode" }),
    ).not.toBeInTheDocument();
  });

  it("keeps the settings away from a member", async () => {
    mount({ apps: [grafana], grants: [] });
    await pad();
    await userEvent.click(
      screen.getByRole("button", { name: "More for Grafana" }),
    );
    await screen.findByRole("menuitem", { name: "Copy link" });
    expect(
      screen.queryByRole("menuitem", { name: "Manage application" }),
    ).toBeNull();
  });

  it("finds an application by name, and says when none matches", async () => {
    mount({ apps: [gitea, grafana], grants: [grant("oidc", "gitea")] });
    await pad();
    const search = screen.getByRole("searchbox", { name: "Search your apps" });

    await userEvent.type(search, "graf");
    expect(await tileNames()).toEqual(["Open Grafana in a new tab"]);
    // Add app waits while a search narrows the pad.
    expect(screen.queryByRole("button", { name: /Add app/ })).toBeNull();

    await userEvent.clear(search);
    await userEvent.type(search, "zzz");
    expect(await screen.findByRole("status")).toHaveTextContent(
      "No apps match “zzz”.",
    );
  });

  it("goes to the search with / from anywhere on the page", async () => {
    mount({ apps: [grafana], grants: [] });
    await pad();
    await userEvent.keyboard("/");
    expect(
      screen.getByRole("searchbox", { name: "Search your apps" }),
    ).toHaveFocus();
  });

  it("asks the member to connect one when nothing is connected yet", async () => {
    mount({ apps: [wiki], grants: [] });
    expect(
      await screen.findByText(
        "No connected apps yet. Choose Add app to connect one.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Add app/ })).toBeInTheDocument();
  });

  it("says so when the account may use no application", async () => {
    mount({ apps: [], grants: [] });
    expect(await screen.findByText("No applications yet")).toBeInTheDocument();
  });

  it("reports a failed grant read as the page's failure, once", async () => {
    mount({
      apps: [gitea],
      grants: [],
      routes: {
        "GET /api/prohibitorum/me/consent": () =>
          apiError("internal_error", 500),
      },
    });
    expect(
      await screen.findByText("Your applications could not be loaded"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Try again" }),
    ).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });
});
