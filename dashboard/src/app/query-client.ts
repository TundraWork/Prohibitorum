import { MutationCache, QueryCache, QueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";

export function createQueryClient(notifyError: (error: unknown) => void) {
  const onError = (error: unknown) => {
    if (!isCancellation(error)) notifyError(error);
  };
  return new QueryClient({
    queryCache: new QueryCache({ onError }),
    mutationCache: new MutationCache({ onError }),
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
}
