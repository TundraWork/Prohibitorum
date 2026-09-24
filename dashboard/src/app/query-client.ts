import type { MessageDescriptor } from "@lingui/core";
import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: {
      /**
       * What the toast says once the write succeeds. A write whose outcome
       * depends on what was sent passes a function of its variables.
       */
      success?: MessageDescriptor | ((variables: unknown) => MessageDescriptor);
    };
  }
}

export function createQueryClient(
  notifyError: (error: unknown) => void,
  notifySuccess: (message: MessageDescriptor) => void = () => undefined,
) {
  const onError = (error: unknown) => {
    if (!isCancellation(error)) notifyError(error);
  };
  return new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({
      onError,
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
}
