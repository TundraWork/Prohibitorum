import { type QueryClient, queryOptions } from "@tanstack/react-query";
import { client, requireJsonData } from "@/api/client";

export function publicConfigQueryOptions() {
  return queryOptions({
    queryKey: ["public", "config"],
    staleTime: 30_000,
    gcTime: 300_000,
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/config", { signal })),
  });
}

export function authStatusQueryOptions() {
  return queryOptions({
    queryKey: ["public", "auth-status"],
    staleTime: 30_000,
    gcTime: 300_000,
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/auth/status", { signal })),
  });
}

export function sessionQueryOptions() {
  return queryOptions({
    queryKey: ["session", "me"],
    staleTime: 0,
    meta: { requiresSession: true },
    queryFn: ({ signal }) =>
      requireJsonData(client.GET("/api/prohibitorum/me", { signal })),
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
