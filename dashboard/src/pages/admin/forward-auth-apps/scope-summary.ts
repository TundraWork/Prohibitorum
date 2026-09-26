import type { components } from "@/api/generated/schema";

type Scope = components["schemas"]["ForwardAuthScope"];

/**
 * The declared scope vocabulary as one list cell.
 *
 * A scope name is an opaque label the upstream service understands — `read`,
 * `admin:users` — so the first few names tell a reader more about the row than
 * any count could. Past three, the names stop fitting a column and the count is
 * what is left to say, so the rest collapse into `+n`. A vocabulary with
 * nothing in it reads as the empty marker the other list cells use, not as an
 * empty column.
 */
export function scopeSummary(scopes: Scope[] | null | undefined): string {
  const names = (scopes ?? []).map((scope) => scope.name);
  if (names.length === 0) return "—";
  if (names.length <= 3) return names.join(", ");
  return `${names.slice(0, 3).join(", ")} +${names.length - 3}`;
}
