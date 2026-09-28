import { z } from "zod";
import type { AccountFilters } from "@/api/queries";
import { searchChoice, searchText } from "@/app/search-params";

/**
 * The account list's filters, which live in the URL and nowhere else: the page
 * renders what the search string says, the server executes it, and a reload or
 * a shared link lands on the same list.
 *
 * Validation is strict and deliberate. A missing, repeated or unknown value is
 * the empty filter. The server rejects a partial advanced filter (it wants all
 * four of provider, field, value and match) and a `match` outside the operator
 * set the provider publishes, so a half-typed filter is neither sent nor
 * silently repaired: it is kept in the URL so the user can finish it, but
 * `accountFilterQuery` reports it as incomplete and the page leaves it out of
 * the request instead of guessing.
 *
 * No trimming, case folding or synonym matching. A cursor is never accepted
 * here: pagination is "keep loading" within one result set, and a link that
 * dropped a reader into the middle of one would be answering a question they
 * did not ask.
 */

/** The operator set `pkg/federation` publishes; the server accepts no others. */
const matchOperators = ["exact", "prefix", "contains"] as const;
export type MatchOperator = (typeof matchOperators)[number];

const roles = ["user", "admin"] as const;
export type RoleFilter = (typeof roles)[number];

export const userSearch = z.object({
  q: searchText(),
  provider: searchText(),
  field: searchText(),
  value: searchText(),
  match: searchChoice(["", ...matchOperators], ""),
  role: searchChoice(["", ...roles], ""),
  /** `disabled`, `enabled`, or unset for both. */
  state: searchChoice(["", "enabled", "disabled"], ""),
});

export type UserFilters = z.output<typeof userSearch>;

/**
 * The filters as the server takes them, or the reason it cannot take them yet.
 *
 * `role` and `state` are not query parameters: `GET /accounts` has no such
 * filter, and the card's design notes name them anyway. Rather than send
 * something the server ignores — which would look like a filter that quietly
 * does nothing — they are applied to the rows already in hand and the list is
 * told that is what is happening.
 */
export function accountFilterQuery(filters: UserFilters): AccountFilters {
  const advanced = {
    provider: filters.provider,
    field: filters.field,
    value: filters.value,
    match: filters.match,
  };
  const completeAdvanced =
    advanced.provider !== "" &&
    advanced.field !== "" &&
    advanced.value !== "" &&
    advanced.match !== "";
  return {
    ...(filters.q === "" ? {} : { q: filters.q }),
    ...(completeAdvanced ? advanced : {}),
  };
}

/** Whether an advanced filter is being built but is not yet sendable. */
export function advancedFilterIncomplete(filters: UserFilters): boolean {
  const parts = [filters.provider, filters.field, filters.value, filters.match];
  return parts.some((part) => part !== "") && parts.some((part) => part === "");
}
