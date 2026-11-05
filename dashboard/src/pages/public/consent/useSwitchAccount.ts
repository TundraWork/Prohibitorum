import { useMutation } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { switchAccountMutationOptions } from "@/api/mutations";
import { followRedirect } from "@/app/redirect";

/**
 * "Use another account" on a consent page: the request is kept and the
 * account chooser opens for it, so the reader can pick another account on
 * this browser or sign one in. Nothing is signed out. The button stays
 * pending until the chooser has opened.
 */
export function useSwitchAccount() {
  const router = useRouter();
  return useMutation({
    ...switchAccountMutationOptions(),
    onSuccess: async ({ redirect }) => {
      await followRedirect(router, redirect);
    },
  });
}
