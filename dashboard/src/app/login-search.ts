import { z } from "zod";
import { rawSearchValue } from "@/app/search-params";

/**
 * The sign-in steps' search values, kept as the address carried them. The
 * steps carry the whole search string from one to the next, so both of these
 * reach the second step.
 *
 * `return_to` is read through `readReturnTo`, which refuses a link that names
 * it twice. `admin` is the administrators' way in during maintenance: the key
 * counts whatever its value, `?admin`, `?admin=0` and a repeated one included
 * (see `maintenanceRedirect`).
 */
export const loginSearch = z.object({
  return_to: rawSearchValue,
  admin: rawSearchValue,
});

/** The sign-in steps' loader reads the raw `return_to`. */
export function loginLoaderDeps({
  search,
}: {
  search: z.output<typeof loginSearch>;
}) {
  return { returnTo: search.return_to };
}
