import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";

/**
 * Signs out and opens the sign-in page, which comes back to `returnTo` once
 * another account has signed in. The consent and require-identity pages offer
 * it to someone signed in as the wrong account.
 */
export function useSwitchAccount(returnTo: string) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      await navigate({ to: "/login", search: { return_to: returnTo } });
    },
  });
}
