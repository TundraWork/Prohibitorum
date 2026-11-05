import { z } from "zod";
import { optionalSearchText, rawSearchValue } from "@/app/search-params";

/**
 * The sign-in steps' search values, kept as the address carried them. The
 * steps carry the whole search string from one to the next, so both of these
 * reach the second step.
 *
 * `return_to` is read through `readReturnTo`, which refuses a link that names
 * it twice. `admin` is the administrators' way in during maintenance: the key
 * counts whatever its value, `?admin`, `?admin=0` and a repeated one included
 * (see `maintenanceRedirect`).
 *
 * `login_hint` fills in the username: the account a protocol request or a
 * signed-out tab names. `add` keeps the page open while an account is signed
 * in, to add another to the browser; like `admin`, the key counts whatever
 * its value.
 */
export const loginSearch = z.object({
  return_to: rawSearchValue,
  admin: rawSearchValue,
  login_hint: optionalSearchText(),
  add: rawSearchValue,
});

/** The sign-in steps' loader reads the raw `return_to`. */
export function loginLoaderDeps({
  search,
}: {
  search: z.output<typeof loginSearch>;
}) {
  return { returnTo: search.return_to, add: search.add !== undefined };
}
