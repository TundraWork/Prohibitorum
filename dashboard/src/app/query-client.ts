import type { MessageDescriptor } from "@lingui/core";
import {
  isCancelledError,
  MutationCache,
  QueryCache,
  QueryClient,
  type QueryKey,
} from "@tanstack/react-query";
import { ApiError, type ErrorScope, isCancellation } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";

declare module "@tanstack/react-query" {
  interface Register {
    queryMeta: {
      /** Marks a query for the signed-in account; see `clearSessionQueries`. */
      requiresSession?: boolean;
      /**
       * A read the page repeats on its own, whose failure is therefore not
       * reported: the next read is already on its way.
       */
      quiet?: boolean;
    };
    mutationMeta: {
      /**
       * What the toast says once the write succeeds. A write whose outcome
       * depends on what was sent, or on what came back, passes a function of
       * its variables and its result.
       */
      success?:
        | MessageDescriptor
        | ((variables: unknown, data: unknown) => MessageDescriptor);
      /**
       * Where the write was made, for an error code whose wording depends on
       * it; see `describeError`.
       */
      errorScope?: ErrorScope;
      /**
       * The page shows this write's failure itself, as an alert or a field
       * error, so the toast would say it twice. `true` covers every failure;
       * a function picks the ones the page has a place for.
       */
      reportsInline?: boolean | ((error: unknown) => boolean);
    };
  }
}

function isMaintenance(error: unknown): boolean {
  return error instanceof ApiError && error.code === "maintenance_mode";
}

function isShownInline(
  error: unknown,
  reportsInline: boolean | ((error: unknown) => boolean) | undefined,
): boolean {
  return typeof reportsInline === "function"
    ? reportsInline(error)
    : reportsInline === true;
}

/** How many times a read whose fetch keeps being given up asks again. */
const givenUpReadAttempts = 3;

/**
 * Keeps an imperative read — a route loader's `fetchQuery` or `query`, and
 * `ensureQueryData` and `prefetchQuery` through them — from failing because
 * the fetch it shared was given up.
 *
 * Such a read joins a fetch already on its way for the same query, and gets
 * that fetch's `CancelledError` when TanStack Query gives it up, even though
 * the query still holds data: when the last component watching the query
 * unmounts, as the page being left does while the next page's loaders run, or
 * when an invalidation replaces it with a newer fetch. A loader that throws it
 * fails the page it was loading. Such a read asks again. One cancelled on
 * purpose still fails: through `cancelQueries`, as signing out cancels the
 * session reads before removing them, or by removing the query.
 */
function outlastGivenUpFetches(queryClient: QueryClient) {
  const cache = queryClient.getQueryCache();
  // The queries `cancelQueries` is cancelling, until their fetches settle.
  const cancelledOnPurpose = new WeakSet<object>();

  async function untilSettled<T>(
    queryKey: QueryKey,
    read: () => Promise<T>,
  ): Promise<T> {
    for (let attempt = 1; ; attempt += 1) {
      try {
        return await read();
      } catch (error) {
        const query = cache.find({ queryKey, exact: true });
        const givenUp = query !== undefined && !cancelledOnPurpose.has(query);
        if (
          !isCancelledError(error) ||
          !givenUp ||
          attempt >= givenUpReadAttempts
        ) {
          throw error;
        }
      }
    }
  }

  const fetchQuery = queryClient.fetchQuery.bind(queryClient);
  queryClient.fetchQuery = ((options: { queryKey: QueryKey }) =>
    untilSettled(options.queryKey, () =>
      fetchQuery(options),
    )) as QueryClient["fetchQuery"];
  const query = queryClient.query.bind(queryClient);
  queryClient.query = ((options: { queryKey: QueryKey }) =>
    untilSettled(options.queryKey, () =>
      query(options),
    )) as QueryClient["query"];
  const cancelQueries = queryClient.cancelQueries.bind(queryClient);
  queryClient.cancelQueries = (async (
    ...args: Parameters<QueryClient["cancelQueries"]>
  ) => {
    const cancelled = cache.findAll(args[0]);
    for (const query of cancelled) cancelledOnPurpose.add(query);
    try {
      await cancelQueries(...args);
    } finally {
      for (const query of cancelled) cancelledOnPurpose.delete(query);
    }
  }) as QueryClient["cancelQueries"];
}

/**
 * The console's query client. Every failed read and write is reported through
 * `notifyError`, apart from a cancellation, and apart from `maintenance_mode`:
 * maintenance that began while a page was open is not a failure of the thing
 * the reader pressed, so the cached config learns that maintenance is on and
 * `onMaintenance` takes the reader to the maintenance page. A read marked
 * `quiet`, and a write failure the page shows inline, report nothing but
 * maintenance. An imperative read survives a fetch it shared being given up;
 * see `outlastGivenUpFetches`.
 */
export function createQueryClient(
  notifyError: (error: unknown, scope?: ErrorScope) => void,
  notifySuccess: (message: MessageDescriptor) => void = () => undefined,
  onMaintenance: () => void = () => undefined,
) {
  const onError = (error: unknown, scope?: ErrorScope) => {
    if (isCancellation(error)) return;
    if (isMaintenance(error)) {
      queryClient.setQueryData(publicConfigQueryOptions().queryKey, (config) =>
        config ? { ...config, maintenanceMode: true } : config,
      );
      onMaintenance();
      return;
    }
    notifyError(error, scope);
  };
  const queryClient = new QueryClient({
    queryCache: new QueryCache({
      onError: (error, query) => {
        if (query.meta?.quiet === true && !isMaintenance(error)) return;
        onError(error);
      },
    }),
    mutationCache: new MutationCache({
      onError: (error, _variables, _context, mutation) => {
        if (
          !isMaintenance(error) &&
          isShownInline(error, mutation.meta?.reportsInline)
        )
          return;
        onError(error, mutation.meta?.errorScope);
      },
      // A write that changes something says so. It is announced from the cache
      // rather than the page, so the toast still arrives when the dialog that
      // started the write has already closed.
      onSuccess: (data, variables, _context, mutation) => {
        const success = mutation.meta?.success;
        if (success === undefined) return;
        notifySuccess(
          typeof success === "function" ? success(variables, data) : success,
        );
      },
    }),
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  outlastGivenUpFetches(queryClient);
  return queryClient;
}

/** Marks a read as one whose failure the page draws itself, so no toast repeats it. */
export function quietQuery<O extends object>(
  options: O,
): O & { meta: Record<string, unknown> } {
  const { meta } = options as { meta?: Record<string, unknown> };
  return { ...options, meta: { ...meta, quiet: true } };
}

/** Marks a write as one whose failure the page shows itself; see `reportsInline`. */
export function withInlineReport<O extends { meta?: Record<string, unknown> }>(
  options: O,
  reportsInline: boolean | ((error: unknown) => boolean),
): O {
  return { ...options, meta: { ...options.meta, reportsInline } };
}
