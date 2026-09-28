import { text } from "@/app/search";
import type { SearchValue } from "@/app/search-params";

export interface LoginSearch {
  return_to?: string;
  /**
   * Present on the administrators' way in during maintenance, whatever its
   * value: `?admin`, `?admin=1` and `?admin=0` all count (see
   * `maintenanceRedirect`).
   */
  admin?: SearchValue;
}

/**
 * The sign-in steps' search values. The steps carry the whole search string
 * from one to the next, so both of these reach the second step.
 *
 * The sign-in page reads `return_to` through `readReturnTo`, which refuses a
 * link that names it twice; this only types it for the links that build a
 * sign-in address.
 */
export function loginSearch(search: Record<string, unknown>): LoginSearch {
  const returnTo = text(search.return_to);
  return {
    ...(returnTo === undefined ? {} : { return_to: returnTo }),
    ...(Object.hasOwn(search, "admin")
      ? { admin: search.admin as SearchValue }
      : {}),
  };
}
