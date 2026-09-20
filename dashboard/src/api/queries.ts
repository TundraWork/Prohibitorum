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
