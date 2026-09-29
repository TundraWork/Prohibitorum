import type { QueryClient } from "@tanstack/react-query";
import { screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

type OidcApp = components["schemas"]["OIDCApplicationView"];
type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
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
    scopes: [],
    accessRestricted: false,
    disabled: false,
    remoteUserSource: "username",
    createdAt,
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
  },
) {
  const oidc = apps.oidc ?? [];
  const forwardAuth = apps.forwardAuth ?? [];
  const base = "/api/prohibitorum";
  return fakeApi({
    ...publicApi({ ...testSession, role }),
    [`GET ${base}/oidc-applications`]: page(oidc),
    [`GET ${base}/saml-applications`]: page([]),
    [`GET ${base}/forward-auth-apps`]: page(forwardAuth),
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
