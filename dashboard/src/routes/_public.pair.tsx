import { createFileRoute } from "@tanstack/react-router";
import { readReturnTo } from "@/api/auth";
import { loginSearch } from "@/app/login-search";
import { PairPage } from "@/pages/public/Pair";

/**
 * Signing this device in from another one that is signed in already, reached
 * from the sign-in page. The page takes the sign-in page's whole search and
 * hands it back on the way out, so returning keeps `add`, `login_hint` and
 * `admin` as well as `return_to`: without `add`, a browser with an account
 * signed in would be sent from the sign-in page to its home. A link that names
 * `return_to` twice is refused the same way. A browser with an account signed
 * in stays here too: pairing adds another account to it, as "Add account"
 * does.
 *
 * The loader only reads: the pairing itself is started by the page, because
 * hovering the sign-in page's link runs this loader too.
 */
export const Route = createFileRoute("/_public/pair")({
  validateSearch: loginSearch,
  loaderDeps: ({ search: { return_to } }) => ({ returnTo: return_to }),
  loader: ({ deps }) => {
    readReturnTo(deps.returnTo);
  },
  component: PairPage,
});
