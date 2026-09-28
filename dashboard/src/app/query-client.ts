import type { MessageDescriptor } from "@lingui/core";
import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
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
       * depends on what was sent passes a function of its variables.
       */
      success?: MessageDescriptor | ((variables: unknown) => MessageDescriptor);
      /**
       * Where the write was made, for an error code whose wording depends on
       * it; see `describeError`.
       */
      errorScope?: ErrorScope;
    };
  }
}

function isMaintenance(error: unknown): boolean {
  return error instanceof ApiError && error.code === "maintenance_mode";
}

/**
 * The console's query client. Every failed read and write is reported through
 * `notifyError`, apart from a cancellation, and apart from `maintenance_mode`:
 * maintenance that began while a page was open is not a failure of the thing
 * the reader pressed, so the cached config learns that maintenance is on and
 * `onMaintenance` takes the reader to the maintenance page. A read marked
 * `quiet` reports nothing but maintenance.
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
      onError: (error, _variables, _context, mutation) =>
        onError(error, mutation.meta?.errorScope),
      // A write that changes something says so. It is announced from the cache
      // rather than the page, so the toast still arrives when the dialog that
      // started the write has already closed.
      onSuccess: (_data, variables, _context, mutation) => {
        const success = mutation.meta?.success;
        if (success === undefined) return;
        notifySuccess(
          typeof success === "function" ? success(variables) : success,
        );
      },
    }),
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return queryClient;
}
