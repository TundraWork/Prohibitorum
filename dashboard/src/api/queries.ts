import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { client, requireJsonData } from "@/api/client";
import { ApiError } from "@/api/errors";

export function publicConfigQueryOptions() {
  return queryOptions({
    queryKey: ["public", "config"],
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/config", { signal })),
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
 * The first page of accounts matching a search, for a picker. Kept apart from
 * `accountsListOptions`, whose key holds the directory's accumulated pages.
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

export function invitationsQueryOptions(cursor?: string) {
  return queryOptions({
    queryKey: ["admin", "invitations", cursor ?? ""] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/invitations", {
          // Omitted rather than sent empty: an empty `cursor` is not the same
          // request as no cursor at all.
          ...(cursor === undefined ? {} : { params: { query: { cursor } } }),
          signal,
        }),
      ),
  });
}

/**
 * The same read, shaped for `useCursorList`: one page per issued cursor, so the
 * list can walk invitations without rebuilding the query context by hand.
 */
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
export function clientIpQueryOptions() {
  return queryOptions({
    queryKey: ["admin", "settings", "client-ip"] as const,
    queryFn: ({ signal }) =>
      requireJsonData(
        client.GET("/api/prohibitorum/admin/settings/client-ip", { signal }),
      ),
  });
}
