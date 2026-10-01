import {
  type Query,
  type QueryClient,
  queryOptions,
} from "@tanstack/react-query";
import { client, requireJsonData } from "@/api/client";
import { readEnrollmentPreview } from "@/api/enrollment";
import { ApiError } from "@/api/errors";
import { readFederationFlow } from "@/api/federation";
import type {
  ManagedApplicationKind,
  WallpaperPreviewQuery,
} from "@/api/raw-admin-paths";
import type {
  FederationConfirm,
  LoginAppearance,
  PairingStatus,
} from "@/api/raw-paths";

export function publicConfigQueryOptions() {
  return queryOptions({
    queryKey: ["public", "config"],
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/config", { signal })),
  });
}

/**
 * The Bing or Unsplash picture behind the sign-in page. Read once per visit —
 * the page keeps the picture it opened with — and a failure is not reported:
 * the page simply keeps its own background. The server answers for the saved
 * source; the key carries it, so a newly saved source is a new read rather
 * than the old source's picture.
 */
export function loginWallpaperQueryOptions(
  background: LoginAppearance["background"],
) {
  const params =
    background.source === "bing"
      ? [background.source, background.bing.market]
      : [background.source, background.unsplash.query];
  return queryOptions({
    queryKey: ["public", "login-wallpaper", ...params],
    staleTime: Number.POSITIVE_INFINITY,
    refetchOnWindowFocus: false,
    retry: false,
    meta: { quiet: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/branding/wallpaper", { signal })),
  });
}

export function authStatusQueryOptions() {
  return queryOptions({
    queryKey: ["public", "auth-status"],
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/auth/status", { signal })),
  });
}

export function sessionQueryOptions() {
  return queryOptions({
    queryKey: ["session", "me"],
    meta: { requiresSession: true },
    queryFn: async ({ signal }) => {
      try {
        return await requireJsonData(
          client.GET("/api/prohibitorum/me", { signal }),
        );
      } catch (error) {
        if (
          error instanceof ApiError &&
          error.status === 401 &&
          error.code === "no_session"
        ) {
          return null;
        }
        throw error;
      }
    },
  });
}

export async function clearSessionQueries(queryClient: QueryClient) {
  const filters = {
    predicate: (query: { meta?: Record<string, unknown> }) =>
      query.meta?.requiresSession === true,
  };
  await queryClient.cancelQueries(filters);
  queryClient.removeQueries(filters);
}

/**
 * The OIDC authorization a consent ticket stands for. Reading it only looks at
 * the ticket; the decision is what uses it up. It is never served from cache,
 * because a ticket decided in another tab is gone.
 */
export function consentRequestQueryOptions(ticket: string) {
  return queryOptions({
    queryKey: ["consent-request", ticket],
    staleTime: 0,
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/consent", {
          params: { query: { ticket } },
          signal,
        }),
      ),
  });
}

/** The SAML sign-in a consent ticket stands for; see `consentRequestQueryOptions`. */
export function samlConsentRequestQueryOptions(ticket: string) {
  return queryOptions({
    queryKey: ["saml-consent-request", ticket],
    staleTime: 0,
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/saml-consent", {
          params: { query: { ticket } },
          signal,
        }),
      ),
  });
}

/**
 * What an enrollment link will set up. Never served from cache, and not read
 * again on focus: the link is used up once the account exists, and the
 * passkey prompt takes focus away while that happens.
 */
export function enrollmentQueryOptions(token: string) {
  return queryOptions({
    queryKey: ["public", "enrollment", token],
    staleTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      readEnrollmentPreview(
        await requireJsonData(
          client.GET("/api/prohibitorum/enrollments/{token}", {
            params: { path: { token } },
            signal,
          }),
        ),
      ),
  });
}

/**
 * The account a first sign-in through a provider has prepared, read from the
 * browser's federation cookie. Confirming uses the grant up, so it is not read
 * again on focus.
 */
export function federationConfirmQueryOptions() {
  return queryOptions({
    queryKey: ["public", "federation-confirm"],
    staleTime: 0,
    refetchOnWindowFocus: false,
    queryFn: ({ signal }): Promise<FederationConfirm> =>
      requireJsonData(
        client.GET("/api/prohibitorum/auth/federation/confirm", { signal }),
      ),
  });
}

/** How often a waiting pairing is asked whether it has been approved. */
export const pairingPollMs = 2500;

/**
 * Where a pairing this device started stands, read every 2.5 seconds while it
 * is still waiting for the other device and not after. A failed read is not
 * reported: the next one is on its way, and the page keeps waiting.
 */
export function pairingStatusQueryOptions(id: string) {
  return queryOptions({
    queryKey: ["public", "pairing-status", id],
    staleTime: 0,
    refetchOnWindowFocus: false,
    meta: { quiet: true },
    refetchInterval: (query: Query<PairingStatus, Error>) =>
      query.state.data === undefined || query.state.data.status === "pending"
        ? pairingPollMs
        : false,
    queryFn: ({ signal }): Promise<PairingStatus> =>
      requireJsonData(
        client.GET("/api/prohibitorum/auth/devices/pair/status", {
          params: { query: { id } },
          signal,
        }),
      ),
  });
}

/**
 * A VRChat profile verification. Its step moves on the page's own writes,
 * which put the flow they return in the cache, and verifying finishes it, so
 * it is not read again on focus.
 */
export function federationFlowQueryOptions(flow: string) {
  return queryOptions({
    queryKey: ["public", "federation-flow", flow],
    staleTime: 0,
    refetchOnWindowFocus: false,
    queryFn: async ({ signal }) =>
      readFederationFlow(
        await requireJsonData(
          client.GET("/api/prohibitorum/auth/federation/flows/{flow}", {
            params: { path: { flow } },
            signal,
          }),
        ),
      ),
  });
}

/**
 * The providers the sign-in page offers. The list is public, so unlike
 * `federationProvidersQueryOptions` it does not belong to a session.
 */
export function publicFederationProvidersQueryOptions() {
  return queryOptions({
    queryKey: ["public", "federation-providers"],
    queryFn: async ({ signal }) =>
      (await requireJsonData(
        client.GET("/api/prohibitorum/auth/federation", { signal }),
      )) ?? [],
  });
}

/**
 * The pictures the account can show and the one it shows: its own upload
 * first, then each provider's, as the server stores them. The sidebar reads the
 * picture in use from the session instead, so a write refreshes both.
 */
export function myAvatarQueryOptions() {
  return queryOptions({
    queryKey: ["session", "avatar"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me/avatar", { signal })),
  });
}

export function credentialsQueryOptions() {
  return queryOptions({
    queryKey: ["session", "credentials"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/me/credentials", { signal }),
      ),
  });
}

export function factorsQueryOptions() {
  return queryOptions({
    queryKey: ["session", "factors"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me/factors", { signal })),
  });
}

export function sessionsQueryOptions() {
  return queryOptions({
    queryKey: ["session", "sessions"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me/sessions", { signal })),
  });
}

export function identitiesQueryOptions() {
  return queryOptions({
    queryKey: ["session", "identities"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/me/identities", { signal }),
      ),
  });
}

export function tokensQueryOptions() {
  return queryOptions({
    queryKey: ["session", "tokens"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me/tokens", { signal })),
  });
}

export function forwardAuthAppsQueryOptions() {
  return queryOptions({
    queryKey: ["session", "forward-auth-apps"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/me/forward-auth-apps", { signal }),
      ),
  });
}

export function consentQueryOptions() {
  return queryOptions({
    queryKey: ["session", "consent"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me/consent", { signal })),
  });
}

export function federationProvidersQueryOptions() {
  return queryOptions({
    queryKey: ["session", "federation-providers"],
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/auth/federation", { signal }),
      ),
  });
}

/* ---------------------------------------------------------------- admin -- */

/**
 * Management reads deliberately carry no `meta: { requiresSession: true }`.
 * That marker is how signing out finds the queries holding *this session's*
 * private data; the admin directory belongs to the instance, not to whoever is
 * signed in, so a sign-out leaves it cached rather than cancelling and removing
 * it (`clearSessionQueries`).
 */

/** The filters `GET /accounts` accepts; all optional and all server-side. */
export interface AccountFilters {
  q?: string;
  provider?: string;
  field?: string;
  value?: string;
  match?: string;
}

/**
 * One page of the account directory. Written for `useCursorList`, which walks
 * the pages one issued cursor at a time; `filters` is what the reader asked
 * for, and the cursor is where the previous page stopped.
 */
export function accountsListOptions(filters: AccountFilters) {
  return {
    queryKey: ["admin", "accounts", filters] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts", {
          params: {
            query: cursor === undefined ? filters : { ...filters, cursor },
          },
          signal,
        }),
      ),
  };
}

/**
 * The first page of accounts matching a search, for a picker. A plain read of
 * its own, not `accountsListOptions`: that key belongs to the directory's
 * infinite query, and only `useCursorList` may read it.
 */
export function accountSearchQueryOptions(q: string) {
  return queryOptions({
    queryKey: ["admin", "accounts", "search", q] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts", {
          params: { query: q === "" ? {} : { q } },
          signal,
        }),
      ),
  });
}

export function accountQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "accounts", id] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts/{id}", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

/** A bare array, not a `{ items, nextCursor }` envelope. */
export function accountIdentitiesQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "accounts", id, "identities"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts/{id}/identities", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

export function accountCredentialsQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "accounts", id, "credentials"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts/{id}/credentials", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

export function accountSessionsQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "accounts", id, "sessions"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts/{id}/sessions", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

export function accountTokensQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "accounts", id, "tokens"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/accounts/{id}/tokens", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

/** One page of invitations, written for `useCursorList`. */
export function invitationsListOptions() {
  return {
    queryKey: ["admin", "invitations"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/invitations", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

/** Providers carry the search fields and match operators the filter offers. */
export function identityProvidersQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "identity-providers"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/identity-providers", { signal }),
      ),
  });
}

/** A bare array: the admin's whole directory, or a non-admin's memberships. */
export function groupsQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "groups"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/groups", { signal })),
  });
}

/* ------------------------------------------------------------- federation -- */

/**
 * One page of identity providers, shaped for `useCursorList`. The cursor is
 * threaded through as the page parameter and never reaches the URL.
 */
export function identityProvidersListOptions() {
  return {
    queryKey: ["admin", "identity-providers"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/identity-providers", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

export function identityProviderQueryOptions(slug: string) {
  return queryOptions({
    queryKey: ["admin", "identity-providers", slug] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/identity-providers/{slug}", {
          params: { path: { slug } },
          signal,
        }),
      ),
  });
}

/**
 * The resolved OIDC configuration, fetched only when the reader asks: the
 * endpoint discovers against the upstream and is rate limited to 20 a minute, so
 * it must never be a mount-time read. `staleTime: 0` keeps a second click from
 * serving the first answer back.
 */
export function effectiveConfigQueryOptions(slug: string) {
  return queryOptions({
    queryKey: [
      "admin",
      "identity-providers",
      slug,
      "effective-config",
    ] as const,
    staleTime: 0,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET(
          "/api/prohibitorum/identity-providers/{slug}/effective-config",
          {
            params: { path: { slug } },
            signal,
          },
        ),
      ),
  });
}

/**
 * One diagnostic run's result. The interval lives in the page (it starts when a
 * test is in flight and stops after 30 seconds) because only the page knows
 * whether the run is still being watched; the query itself is a plain read.
 */
export function diagnosticResultQueryOptions(slug: string, id: string) {
  return queryOptions({
    queryKey: ["admin", "identity-providers", slug, "tests", id] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/identity-providers/{slug}/tests/{id}", {
          params: { path: { slug, id } },
          signal,
        }),
      ),
  });
}

/* ------------------------------------------------------- downstream apps -- */

export function oidcAppsListOptions() {
  return {
    queryKey: ["admin", "oidc-applications"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/oidc-applications", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

export function oidcAppQueryOptions(clientId: string) {
  return queryOptions({
    queryKey: ["admin", "oidc-applications", clientId] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/oidc-applications/{clientId}", {
          params: { path: { clientId } },
          signal,
        }),
      ),
  });
}

export function samlAppsListOptions() {
  return {
    queryKey: ["admin", "saml-applications"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/saml-applications", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

export function samlAppQueryOptions(id: number) {
  return queryOptions({
    queryKey: ["admin", "saml-applications", id] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/saml-applications/{id}", {
          params: { path: { id } },
          signal,
        }),
      ),
  });
}

export function forwardAuthAppsListOptions() {
  return {
    queryKey: ["admin", "forward-auth-apps"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/forward-auth-apps", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

export function forwardAuthAppQueryOptions(clientId: string) {
  return queryOptions({
    queryKey: ["admin", "forward-auth-apps", clientId] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/forward-auth-apps/{clientId}", {
          params: { path: { clientId } },
          signal,
        }),
      ),
  });
}

/**
 * The access workspace: the app summary, its restriction flag and its selected
 * groups, in one read. Served by the same handler for admins and for an
 * application's own managers, which is why the access panel is one component.
 */
export function appAccessQueryOptions(
  kind: ManagedApplicationKind,
  appId: string,
) {
  return queryOptions({
    queryKey: ["admin", "managed-applications", kind, appId, "access"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET(
          "/api/prohibitorum/managed-applications/{kind}/{appId}/access",
          {
            params: { path: { kind, appId } },
            signal,
          },
        ),
      ),
  });
}

/**
 * Who may manage this application. The endpoint is admin-only, so a non-admin
 * manager must not call it at all — `AppAccessPanel` decides whether to render
 * the managers block, and this factory is only reached when it does.
 */
export function appManagersQueryOptions(
  kind: ManagedApplicationKind,
  appId: string,
) {
  const path =
    kind === "saml"
      ? ("/api/prohibitorum/saml-applications/{id}/managers" as const)
      : kind === "oidc"
        ? ("/api/prohibitorum/oidc-applications/{clientId}/managers" as const)
        : ("/api/prohibitorum/forward-auth-apps/{clientId}/managers" as const);
  return queryOptions({
    queryKey: [
      "admin",
      "managed-applications",
      kind,
      appId,
      "managers",
    ] as const,
    queryFn: ({ signal }) => {
      if (kind === "saml") {
        return requireJsonData(
          client.GET(path, {
            params: { path: { id: Number(appId) } },
            signal,
          }),
        );
      }
      return requireJsonData(
        client.GET(path, { params: { path: { clientId: appId } }, signal }),
      );
    },
  });
}

/** Which application sections this account may see. */
export interface ManagedApplications {
  oidc: boolean;
  saml: boolean;
  forwardAuth: boolean;
}

/**
 * Whether the signed-in account manages at least one application of each kind.
 *
 * An admin manages all three without asking: the lists would answer "everything"
 * and the sidebar is the same either way. Everyone else gets one row from each
 * list — `limit=1` is enough to know the list is not empty, and the list endpoint
 * already filters to the caller's assignments, so no dedicated endpoint is
 * needed. The three reads run together rather than in sequence.
 */
export function managedApplicationsQueryOptions() {
  return queryOptions({
    queryKey: ["session", "managed-applications"] as const,
    meta: { requiresSession: true },
    queryFn: async ({ signal }) => {
      const [oidc, saml, forwardAuth] = await Promise.all([
        requireJsonData(
          client.GET("/api/prohibitorum/oidc-applications", {
            params: { query: { limit: 1 } },
            signal,
          }),
        ),
        requireJsonData(
          client.GET("/api/prohibitorum/saml-applications", {
            params: { query: { limit: 1 } },
            signal,
          }),
        ),
        requireJsonData(
          client.GET("/api/prohibitorum/forward-auth-apps", {
            params: { query: { limit: 1 } },
            signal,
          }),
        ),
      ]);
      return {
        oidc: (oidc.items?.length ?? 0) > 0,
        saml: (saml.items?.length ?? 0) > 0,
        forwardAuth: (forwardAuth.items?.length ?? 0) > 0,
      } satisfies ManagedApplications;
    },
  });
}

export function groupQueryOptions(groupId: number) {
  return queryOptions({
    queryKey: ["admin", "groups", groupId] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/{groupId}", {
          params: { path: { groupId } },
          signal,
        }),
      ),
  });
}

/** The rule editor's provider vocabulary; includes disabled and invite-only. */
export function groupProvidersQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "groups", "providers"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/providers", { signal }),
      ),
  });
}

export function groupDecisionsQueryOptions(groupId: number, cursor?: string) {
  return queryOptions({
    queryKey: ["admin", "groups", groupId, "decisions", cursor ?? ""] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/{groupId}/decisions", {
          params: {
            path: { groupId },
            query: cursor === undefined ? {} : { cursor },
          },
          signal,
        }),
      ),
  });
}

/**
 * Fixed to one page: the handler always answers with an empty `nextCursor`, so
 * the interface offers no "load more" for it.
 */
export function groupPreviewQueryOptions(groupId: number) {
  return queryOptions({
    queryKey: ["admin", "groups", groupId, "preview"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/{groupId}/preview", {
          params: { path: { groupId } },
          signal,
        }),
      ),
  });
}

export function groupExplainQueryOptions(groupId: number, accountId: number) {
  return queryOptions({
    queryKey: ["admin", "groups", groupId, "explain", accountId] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/{groupId}/explain/{accountId}", {
          params: { path: { groupId, accountId } },
          signal,
        }),
      ),
  });
}

/** Fixed to one page, like the preview. */
export function groupApplicationsQueryOptions(groupId: number) {
  return queryOptions({
    queryKey: ["admin", "groups", groupId, "applications"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/groups/{groupId}/applications", {
          params: { path: { groupId } },
          signal,
        }),
      ),
  });
}

/**
 * The filters `GET /audit-events` takes. `since` and `until` are fixed ISO
 * instants rather than "the last day", because the server binds its cursor to
 * the filters: every page of one answer has to ask the same question.
 */
export interface AuditEventFilters {
  factor?: string;
  event?: string;
  accountId?: number;
  since?: string;
  until?: string;
}

/** One page of the audit log, written for `useCursorList`. */
export function auditEventsListOptions(filters: AuditEventFilters) {
  return {
    queryKey: ["admin", "audit-events", filters] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/audit-events", {
          params: {
            query: cursor === undefined ? filters : { ...filters, cursor },
          },
          signal,
        }),
      ),
  };
}

/** One page of signing keys, written for `useCursorList`. */
export function signingKeysListOptions() {
  return {
    queryKey: ["admin", "signing-keys"] as const,
    queryFn: ({ cursor, signal }: { cursor?: string; signal: AbortSignal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/signing-keys", {
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  };
}

/** The stored client-IP policy; the only instance setting `/config` omits. */
export function loginAppearanceQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "settings", "login-appearance"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/admin/settings/login-appearance", {
          signal,
        }),
      ),
  });
}

/**
 * The picture the settings preview shows for the draft's region or keyword,
 * before it is saved. A failure is the preview's own note, not a toast.
 */
export function loginWallpaperPreviewQueryOptions(
  params: WallpaperPreviewQuery,
) {
  return queryOptions({
    queryKey: ["admin", "settings", "login-wallpaper", params] as const,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
    meta: { quiet: true },
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET(
          "/api/prohibitorum/admin/settings/login-appearance/wallpaper",
          { params: { query: params }, signal },
        ),
      ),
  });
}

export function clientIpQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "settings", "client-ip"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/admin/settings/client-ip", { signal }),
      ),
  });
}
