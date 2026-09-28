import { createFileRoute, redirect } from "@tanstack/react-router";
import { z } from "zod";
import { readReturnTo } from "@/api/auth";
import { sessionQueryOptions } from "@/api/queries";
import { rawSearchValue } from "@/app/search-params";
import { PairPage } from "@/pages/public/Pair";

/**
 * Signing this device in from another one that is signed in already, reached
 * from the sign-in page. `return_to` is kept as the sign-in page carried it,
 * and a link that names it twice is refused the same way.
 *
 * The loader only reads: the pairing itself is started by the page, because
 * hovering the sign-in page's link runs this loader too.
 */
export const Route = createFileRoute("/_public/pair")({
  validateSearch: z.object({ return_to: rawSearchValue }),
  loaderDeps: ({ search: { return_to } }) => ({ returnTo: return_to }),
  loader: async ({ context: { queryClient }, deps, cause }) => {
    // Once the page has signed the device in, the session it now reads must
    // not send it away while it offers a passkey.
    if (cause === "stay") return;
    const returnTo = readReturnTo(deps.returnTo);
    const session = await queryClient.query(sessionQueryOptions());
    if (session !== null && returnTo === undefined) {
      throw redirect({ to: "/", replace: true });
    }
  },
  component: PairPage,
});
