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
