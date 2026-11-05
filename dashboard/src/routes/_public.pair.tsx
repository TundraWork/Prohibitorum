import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { readReturnTo } from "@/api/auth";
import { rawSearchValue } from "@/app/search-params";
import { PairPage } from "@/pages/public/Pair";

/**
 * Signing this device in from another one that is signed in already, reached
 * from the sign-in page. `return_to` is kept as the sign-in page carried it,
 * and a link that names it twice is refused the same way. A browser with an
 * account signed in stays here too: pairing adds another account to it, as
 * "Add account" does.
 *
 * The loader only reads: the pairing itself is started by the page, because
 * hovering the sign-in page's link runs this loader too.
 */
export const Route = createFileRoute("/_public/pair")({
  validateSearch: z.object({ return_to: rawSearchValue }),
  loaderDeps: ({ search: { return_to } }) => ({ returnTo: return_to }),
  loader: ({ deps }) => {
    readReturnTo(deps.returnTo);
  },
  component: PairPage,
});
