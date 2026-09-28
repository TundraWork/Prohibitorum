/**
 * A search value a page may read: the string the address carried, or nothing.
 *
 * The router parses the search string before `validateSearch` sees it, and
 * turns a value that looks like a number, a boolean or JSON into one. A page
 * that expects text treats such a value as not given rather than converting
 * it back, because the original spelling is already lost (`12e45678` arrives
 * as a number). Which parser the router should use is PHB-94's question.
 */
export function text(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

/**
 * The named search values that arrived as strings. A value that did not is
 * left out rather than set to `undefined`, so a link to the route does not have
 * to name a value that does not exist.
 */
export function textParams<const K extends string>(
  search: Record<string, unknown>,
  keys: readonly K[],
): Partial<Record<K, string>> {
  const result: Partial<Record<K, string>> = {};
  for (const key of keys) {
    const value = text(search[key]);
    if (value !== undefined) result[key] = value;
  }
  return result;
}
