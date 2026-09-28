import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { ApiError } from "@/api/errors";
import { identitiesQueryOptions, sessionQueryOptions } from "@/api/queries";
import { isSitePath } from "@/app/redirect";
import { optionalSearchText } from "@/app/search-params";
import { SetupSigninPage } from "@/pages/public/SetupSignin";

/**
 * Offered right after a first sign-in through an upstream provider, when the
 * account has no sign-in of its own yet. `redirect` is where the sign-in was
 * going; the page ends there however the reader answers, so it has to be a
 * path on this site.
 */
export const Route = createFileRoute("/_public/setup-signin")({
  validateSearch: z.object({ redirect: optionalSearchText() }),
  loaderDeps: ({ search: { redirect } }) => ({ redirect }),
  loader: async ({ context: { queryClient }, deps }) => {
    if (!isSitePath(deps.redirect)) {
      throw new ApiError({ kind: "local", code: "invalid_return_to" });
    }
    const session = await queryClient.query(sessionQueryOptions());
    if (session === null) throw redirect({ to: "/login" });
    await queryClient.ensureQueryData(identitiesQueryOptions());
  },
  component: SetupSigninPage,
});
