import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { browserAccountsQueryOptions } from "@/api/queries";
import { optionalSearchText } from "@/app/search-params";
import { SignOutPage } from "@/pages/public/SignOut";

/**
 * Where sso-logout sends a sign-out it cannot tell the reader asked for: a
 * forward-auth application's sign-out without a valid pass. `rd` is the
 * address to return to, which only the server checks; the page writes
 * nothing until the reader answers.
 */
export const Route = createFileRoute("/_public/sign-out")({
  validateSearch: z.object({ rd: optionalSearchText() }),
  // Whether to ask about one account or each of several. A list that cannot
  // be read asks about one, as the page did before there could be several.
  loader: async ({ context: { queryClient } }) => {
    try {
      await queryClient.ensureQueryData(browserAccountsQueryOptions());
    } catch {
      // See above.
    }
  },
  component: SignOutPage,
});
