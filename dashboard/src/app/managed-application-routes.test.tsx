import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { publicConfigQueryOptions } from "@/api/queries";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testConfig,
  testSession,
} from "@/test/app";

type OidcApp = components["schemas"]["OIDCApplicationView"];
type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];
type Provider = components["schemas"]["IdentityProviderView"];

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
  // The config and session every protected route's loader reads, seeded so
  // the loader chain does not wait on them: under parallel CPU load the
  // serial awaits (root config, `_protected` session, `_protected.admin`
  // session again) could straddle the find queries' one-second budget before
  // the page under test even mounted. `publicApi` still answers if anything
  // does ask, and a role the test did not seed is refused by the loaders
  // themselves.
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, testConfig);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

const createdAt = "2026-09-01T00:00:00Z";

function oidcApp(clientId: string, displayName: string): OidcApp {
  return {
    clientId,
    displayName,
    redirectUris: ["https://app.example.test/callback"],
    postLogoutRedirectUris: [],
    allowedScopes: ["openid"],
    clientAuthMethod: "client_secret",
    disabled: false,
    accessRestricted: false,
    subjectSource: "sub",
    claimAliases: {},
    requireConsent: false,
    requirePkce: true,
    createdAt,
  };
}

function forwardAuthApp(clientId: string, displayName: string): ForwardAuthApp {
  return {
    clientId,
    displayName,
    forwardAuthHost: "service.example.test",
    accessRestricted: false,
    disabled: false,
    remoteUserSource: "username",
    createdAt,
  };
}

function provider(slug: string, displayName: string): Provider {
  return {
    slug,
    displayName,
    protocol: "steam",
    mode: "auto_provision",
    allowSignIn: true,
    pendingInvitationCount: null,
    disabled: false,
    ready: true,
    secretConfigured: true,
    secretStatus: "valid",
    secretValidatedAt: null,
    createdAt,
    config: {},
    supportsOperator: false,
    searchFields: [],
    linkedAccountCount: 0,
  };
}

function page<T>(items: T[]) {
  return () => Response.json({ items, nextCursor: "" });
}

/**
 * The reads a signed-in account makes on the way into the management area,
 * plus one application of each kind the account is given. A delegated manager
 * sees only its own rows, which is what the server's lists answer with.
 */
function managementApi(
  role: "user" | "admin",
  apps: {
    oidc?: OidcApp[];
    forwardAuth?: ForwardAuthApp[];
    providers?: Provider[];
  },
) {
  const oidc = apps.oidc ?? [];
  const forwardAuth = apps.forwardAuth ?? [];
  const providers = apps.providers ?? [];
  const base = "/api/prohibitorum";
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: page(oidc),
    [`GET ${base}/saml-applications`]: page([]),
    [`GET ${base}/forward-auth-apps`]: page(forwardAuth),
    [`GET ${base}/identity-providers`]: page(providers),
    ...Object.fromEntries(
      oidc.map((app) => [
        `GET ${base}/oidc-applications/${app.clientId}`,
        () => Response.json(app),
      ]),
    ),
    ...Object.fromEntries(
      forwardAuth.map((app) => [
        `GET ${base}/forward-auth-apps/${app.clientId}`,
        () => Response.json(app),
      ]),
    ),
    ...Object.fromEntries(
      providers.map((item) => [
        `GET ${base}/identity-providers/${item.slug}`,
        () => Response.json(item),
      ]),
    ),
  });
}

it("opens an OIDC application its manager reached from the list", async () => {
  managementApi("user", { oidc: [oidcApp("wiki", "Team wiki")] });
  const router = renderApp("/admin/oidc-applications", queryClient);
  expect(await screen.findByText("Team wiki")).toBeVisible();

  await router.navigate({
    to: "/admin/oidc-applications/$clientId",
    params: { clientId: "wiki" },
  });

  expect(router.state.location.pathname).toBe("/admin/oidc-applications/wiki");
  expect(await screen.findByDisplayValue("Team wiki")).toBeVisible();
});

it("opens a forward-auth application its manager reached from the list", async () => {
  managementApi("user", {
    forwardAuth: [forwardAuthApp("grafana", "Dashboards")],
  });
  const router = renderApp("/admin/forward-auth-apps", queryClient);
  expect(await screen.findByText("Dashboards")).toBeVisible();

  await router.navigate({
    to: "/admin/forward-auth-apps/$clientId",
    params: { clientId: "grafana" },
  });

  expect(router.state.location.pathname).toBe(
    "/admin/forward-auth-apps/grafana",
  );
  expect(await screen.findByDisplayValue("Dashboards")).toBeVisible();
});

it("shows the console's route error for an application the manager was not given", async () => {
  const api = managementApi("user", { oidc: [oidcApp("wiki", "Team wiki")] });
  api.routes["GET /api/prohibitorum/oidc-applications/other-client"] = () =>
    apiError("client_not_found", 404);
  const router = renderApp(
    "/admin/oidc-applications/other-client",
    queryClient,
  );

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "That application no longer exists.",
  );
  expect(router.state.location.pathname).toBe(
    "/admin/oidc-applications/other-client",
  );
  expect(
    api.sent("GET", "/api/prohibitorum/oidc-applications/other-client"),
  ).toHaveLength(1);
});

it("keeps an identity provider named `page` apart from the provider list", async () => {
  managementApi("admin", { providers: [provider("page", "Game accounts")] });
  const router = renderApp("/admin/identity-providers", queryClient);
  expect(await screen.findByText("Game accounts")).toBeVisible();

  await router.navigate({
    to: "/admin/identity-providers/$slug",
    params: { slug: "page" },
  });
  expect(router.state.location.pathname).toBe("/admin/identity-providers/page");
  expect(await screen.findByDisplayValue("Game accounts")).toBeVisible();

  await router.navigate({ to: "/admin/identity-providers" });
  await waitFor(() =>
    expect(router.state.location.pathname).toBe("/admin/identity-providers"),
  );
  expect(await screen.findByText("Game accounts")).toBeVisible();
});

it("keeps an OIDC application with Client ID `page` apart from the application list", async () => {
  managementApi("admin", { oidc: [oidcApp("page", "Team wiki")] });
  const router = renderApp("/admin/oidc-applications", queryClient);
  expect(await screen.findByText("Team wiki")).toBeVisible();

  await router.navigate({
    to: "/admin/oidc-applications/$clientId",
    params: { clientId: "page" },
  });
  expect(router.state.location.pathname).toBe("/admin/oidc-applications/page");
  expect(await screen.findByDisplayValue("Team wiki")).toBeVisible();

  await router.navigate({ to: "/admin/oidc-applications" });
  await waitFor(() =>
    expect(router.state.location.pathname).toBe("/admin/oidc-applications"),
  );
  expect(await screen.findByText("Team wiki")).toBeVisible();
});

it("lets an application's manager allow a provider's identities, and clear one whose provider is gone", async () => {
  const wiki = {
    ...oidcApp("wiki", "Team wiki"),
    allowedScopes: ["openid", "identity:vrchat", "identity:gone"],
  };
  const api = managementApi("user", { oidc: [wiki] });
  api.routes["GET /api/prohibitorum/auth/federation"] = () =>
    Response.json([
      { slug: "vrchat", displayName: "VRChat", protocol: "vrchat" },
      { slug: "steam", displayName: "Steam", protocol: "steam" },
    ]);
  renderApp("/admin/oidc-applications/wiki", queryClient);

  // The provider list loads after the saved scopes, so wait for a box only it
  // brings.
  expect(
    await screen.findByRole("checkbox", { name: "identity:steam" }),
  ).not.toBeChecked();
  expect(
    screen.getByRole("checkbox", { name: "identity:vrchat" }),
  ).toBeChecked();
  expect(screen.getByRole("checkbox", { name: "identity:gone" })).toBeChecked();
  expect(
    screen.getByText("The user's linked VRChat identities."),
  ).toBeVisible();
  expect(
    screen.getByText(
      "This provider is disabled or no longer exists. Clear it to stop allowing it.",
    ),
  ).toBeVisible();
});

it("keeps the saved identity scopes when another scope changes before the provider list arrives", async () => {
  const wiki = {
    ...oidcApp("wiki", "Team wiki"),
    allowedScopes: ["openid", "identity:vrchat"],
  };
  const api = managementApi("user", { oidc: [wiki] });
  api.routes["GET /api/prohibitorum/auth/federation"] = () =>
    new Promise<Response>(() => {});
  api.routes["PUT /api/prohibitorum/oidc-applications/wiki"] = () =>
    Response.json(wiki);
  renderApp("/admin/oidc-applications/wiki", queryClient);

  const groups = await screen.findByRole("checkbox", { name: "groups" });
  expect(
    screen.queryByRole("checkbox", { name: "identity:vrchat" }),
  ).not.toBeInTheDocument();
  await userEvent.click(groups);
  const [save] = screen.getAllByRole("button", { name: "Save" });
  if (!save) throw new Error("no Save button");
  await userEvent.click(save);

  await waitFor(() =>
    expect(
      api.sent("PUT", "/api/prohibitorum/oidc-applications/wiki"),
    ).toHaveLength(1),
  );
  const [put] = api.sent("PUT", "/api/prohibitorum/oidc-applications/wiki");
  if (!put) throw new Error("no PUT was sent");
  expect((put.body as { allowedScopes?: string[] }).allowedScopes).toEqual([
    "openid",
    "groups",
    "identity:vrchat",
  ]);
});

it("tells an admin that deleting a provider clears its identity scope", async () => {
  managementApi("admin", { providers: [provider("vrchat", "VRChat")] });
  renderApp("/admin/identity-providers/vrchat", queryClient);
  await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
  const dialog = await screen.findByRole("alertdialog");
  expect(dialog).toHaveTextContent(
    "Applications allowed identity:vrchat lose that scope, and users' consent to it is cleared. A provider added later with the same identifier needs new consent.",
  );
});
