import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { accountFixture } from "@/test/account";
import {
  apiError,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";
import { forwardAuthAppFixture } from "@/test/forward-auth-app";
import { providerFixture } from "@/test/identity-provider";
import { oidcAppFixture } from "@/test/oidc-app";
import { samlAppFixture } from "@/test/saml-app";

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
const empty = () => Response.json({ items: [], nextCursor: "" });

/**
 * Deletes a record from its own page, with the server answering 404 to every
 * read of it afterwards. The page stays mounted until its navigation runs, so a
 * refetch of the record would raise an error toast for a deletion the reader
 * asked for.
 */
async function deleteFromPage({
  path,
  listPath,
  name,
  detailPath,
  record,
  deletePath,
  confirm,
  extra = {},
}: {
  path: string;
  listPath: string;
  name: string;
  detailPath: string;
  record: unknown;
  deletePath: string;
  confirm: string;
  extra?: Record<string, () => Response>;
}) {
  const user = userEvent.setup();
  let deleted = false;
  const api = fakeApi({
    ...publicApi({ ...testSession, role: "admin" }),
    [`GET ${base}/oidc-applications`]: empty,
    [`GET ${base}/saml-applications`]: empty,
    [`GET ${base}/forward-auth-apps`]: empty,
    [`GET ${base}/identity-providers`]: empty,
    [`GET ${base}/groups`]: () => Response.json([]),
    [`GET ${base}/me/sudo/methods`]: () =>
      Response.json({ methods: ["password_totp"], fresh: true }),
    [`GET ${base}${detailPath}`]: () =>
      deleted ? apiError("not_found", 404) : Response.json(record),
    [`POST ${base}${deletePath}`]: () => {
      deleted = true;
      return new Response(null, { status: 204 });
    },
    ...extra,
  });
  const router = renderApp(path, queryClient);
  await screen.findByRole("heading", { level: 1, name });

  const danger = document.getElementById("danger") as HTMLElement;
  await user.click(within(danger).getByRole("button", { name: "Delete" }));
  const dialog = await screen.findByRole("alertdialog");
  await user.click(within(dialog).getByRole("button", { name: confirm }));

  await waitFor(() => expect(router.state.location.pathname).toBe(listPath));
  expect(api.sent("POST", `${base}${deletePath}`)).toHaveLength(1);
  expect(notifyError).not.toHaveBeenCalled();
}

const access = (kind: string, id: string, name: string) => ({
  [`GET ${base}/managed-applications/${kind}/${id}/access`]: () =>
    Response.json({
      app: { kind, appId: id, displayName: name, accessRestricted: false },
      accessRestricted: false,
      providers: [],
      groups: [],
    }),
});

describe("deleting a record from its own page", () => {
  it("leaves an OIDC application without an error toast", async () => {
    const app = oidcAppFixture();
    await deleteFromPage({
      path: `/admin/oidc-applications/${app.clientId}`,
      listPath: "/admin/oidc-applications",
      name: app.displayName,
      detailPath: `/oidc-applications/${app.clientId}`,
      record: app,
      deletePath: "/oidc-applications/delete",
      confirm: "Delete application",
      extra: {
        ...access("oidc", app.clientId, app.displayName),
        [`GET ${base}/oidc-applications/${app.clientId}/managers`]: () =>
          Response.json([]),
        "GET /.well-known/openid-configuration": () =>
          Response.json({ issuer: "https://id.configured.example.test" }),
      },
    });
  });

  it("leaves a SAML application without an error toast", async () => {
    const app = samlAppFixture();
    await deleteFromPage({
      path: `/admin/saml-applications/${app.id}`,
      listPath: "/admin/saml-applications",
      name: app.displayName,
      detailPath: `/saml-applications/${app.id}`,
      record: app,
      deletePath: "/saml-applications/delete",
      confirm: "Delete application",
      extra: {
        ...access("saml", String(app.id), app.displayName),
        [`GET ${base}/saml-applications/${app.id}/managers`]: () =>
          Response.json([]),
      },
    });
  });

  it("leaves a forward-auth application without an error toast", async () => {
    const app = forwardAuthAppFixture();
    await deleteFromPage({
      path: `/admin/forward-auth-apps/${app.clientId}`,
      listPath: "/admin/forward-auth-apps",
      name: app.displayName,
      detailPath: `/forward-auth-apps/${app.clientId}`,
      record: app,
      deletePath: "/forward-auth-apps/delete",
      confirm: "Delete application",
      extra: {
        ...access("forward_auth", app.clientId, app.displayName),
        [`GET ${base}/forward-auth-apps/${app.clientId}/managers`]: () =>
          Response.json([]),
      },
    });
  });

  it("leaves an identity provider without an error toast", async () => {
    const provider = providerFixture();
    await deleteFromPage({
      path: `/admin/identity-providers/${provider.slug}`,
      listPath: "/admin/identity-providers",
      name: provider.displayName,
      detailPath: `/identity-providers/${provider.slug}`,
      record: provider,
      deletePath: "/identity-providers/delete",
      confirm: "Delete",
    });
  });

  it("leaves an account without an error toast", async () => {
    const account = accountFixture();
    await deleteFromPage({
      path: `/admin/users/${account.id}`,
      listPath: "/admin/users",
      name: account.displayName,
      detailPath: `/accounts/${account.id}`,
      record: account,
      deletePath: "/accounts/delete",
      confirm: "Delete account",
      extra: {
        [`GET ${base}/accounts`]: empty,
        [`GET ${base}/accounts/${account.id}/identities`]: () =>
          Response.json([]),
        [`GET ${base}/accounts/${account.id}/credentials`]: empty,
        [`GET ${base}/accounts/${account.id}/sessions`]: empty,
        [`GET ${base}/accounts/${account.id}/tokens`]: empty,
      },
    });
  });
});
