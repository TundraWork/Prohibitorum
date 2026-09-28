import { z } from "zod";

/**
 * How the router reads and writes the search string, and the fields every
 * route's `validateSearch` is built from.
 *
 * The router's default parser turns a value that looks like a number, a
 * boolean or JSON into one before `validateSearch` sees it, and writes it back
 * that way: `?ref=12e45678` becomes `?ref=Infinity`, and a repeated key becomes
 * a JSON array. These replace it, so a search value is always the text the
 * address carried: a string, or the strings of a repeated key in the order they
 * came. A page that needs a number converts where it reads the value.
 */

export type SearchValue = string | string[];

/** The search string as strings, a repeated key as an array of its values. */
export function parseSearch(searchStr: string): Record<string, SearchValue> {
  const search: Record<string, SearchValue> = Object.create(null);
  for (const [key, value] of new URLSearchParams(searchStr)) {
    const seen = search[key];
    if (seen === undefined) search[key] = value;
    else if (typeof seen === "string") search[key] = [seen, value];
    else seen.push(value);
  }
  return search;
}

/**
 * Writes search values back as they are: a string as one entry, an array as a
 * repeated key, `undefined` not at all. Anything else is refused, because every
 * search value in the app is a string and another type means the caller
 * converted where it should not have.
 */
export function stringifySearch(search: Record<string, unknown>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(search)) {
    if (value === undefined) continue;
    if (typeof value === "string") {
      params.append(key, value);
    } else if (
      Array.isArray(value) &&
      value.every((item) => typeof item === "string")
    ) {
      for (const item of value) params.append(key, item);
    } else {
      const type = Array.isArray(value)
        ? "an array with non-string items"
        : value === null
          ? "null"
          : typeof value;
      throw new TypeError(
        `Search value "${key}" must be a string or an array of strings, not ${type}.`,
      );
    }
  }
  const searchStr = params.toString();
  return searchStr === "" ? "" : `?${searchStr}`;
}

/** Spread into `createRouter` wherever a router is built, tests included. */
export const searchSerialization = { parseSearch, stringifySearch };

/*
 * The fields a route's schema is made of. Each keeps a key optional in links
 * and puts an invalid or repeated value back to its default instead of showing
 * the error page: `.default()` alone throws on an array, and `.catch()` alone
 * makes the key required in links. Zod skips the inner schema when `.default()`
 * applies, so the default need not match a format the schema adds.
 */

/** Text, `""` when missing or invalid. `schema` adds a format. */
export function searchText(schema: z.ZodString = z.string()) {
  return schema.default("").catch("");
}

/** One of `values`, `fallback` when missing or anything else. */
export function searchChoice<const T extends readonly [string, ...string[]]>(
  values: T,
  fallback: T[number],
) {
  return z.enum(values).default(fallback).catch(fallback);
}

/** Text, left out when missing and `undefined` when invalid. */
export function optionalSearchText(schema: z.ZodType<string> = z.string()) {
  return schema.optional().catch(undefined);
}

/** The value as the address carried it, a repeated key included. */
export const rawSearchValue = z
  .union([z.string(), z.array(z.string())])
  .optional();
