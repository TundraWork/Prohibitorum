import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { describe, expect, it } from "vitest";
import { z } from "zod";
import {
  optionalSearchText,
  parseSearch,
  rawSearchValue,
  searchChoice,
  searchSerialization,
  searchText,
  stringifySearch,
} from "@/app/search-params";

describe("parseSearch", () => {
  it("keeps every value as the text the address carried", () => {
    expect(parseSearch("?q=123")).toEqual({ q: "123" });
    expect(parseSearch("?ref=12e45678")).toEqual({ ref: "12e45678" });
    expect(parseSearch("?ref=1e000000")).toEqual({ ref: "1e000000" });
    expect(parseSearch("?q=1.0")).toEqual({ q: "1.0" });
    expect(parseSearch("?q=-0")).toEqual({ q: "-0" });
    expect(parseSearch("?q=true")).toEqual({ q: "true" });
    expect(parseSearch("?q=null")).toEqual({ q: "null" });
    expect(parseSearch("?admin=%221%22")).toEqual({ admin: '"1"' });
  });

  it("reads a key without a value as an empty string", () => {
    expect(parseSearch("?admin")).toEqual({ admin: "" });
    expect(parseSearch("?admin=")).toEqual({ admin: "" });
  });

  it("collects a repeated key in the order it came", () => {
    expect(parseSearch("?a=1&b=2&a=3")).toEqual({ a: ["1", "3"], b: "2" });
    expect(parseSearch("?a=1&a=2&a=3")).toEqual({ a: ["1", "2", "3"] });
  });

  it("decodes `+` and percent escapes", () => {
    expect(parseSearch("?q=a+b")).toEqual({ q: "a b" });
    expect(parseSearch("?q=a%20b")).toEqual({ q: "a b" });
    expect(parseSearch("q=%2Fapps")).toEqual({ q: "/apps" });
  });

  it("treats `__proto__` as an ordinary key", () => {
    const search = parseSearch("?__proto__=x");
    expect(Object.getOwnPropertyDescriptor(search, "__proto__")?.value).toBe(
      "x",
    );
    expect(Object.getPrototypeOf(search)).toBeNull();
  });

  it("reads an empty search string as no values", () => {
    expect(parseSearch("")).toEqual({});
    expect(parseSearch("?")).toEqual({});
  });
});

describe("stringifySearch", () => {
  it("writes strings and arrays back as they are", () => {
    expect(stringifySearch({ q: "123" })).toBe("?q=123");
    expect(stringifySearch({ q: "" })).toBe("?q=");
    expect(stringifySearch({ a: ["1", "3"], b: "2" })).toBe("?a=1&a=3&b=2");
    expect(stringifySearch({ q: "a b", r: "/apps" })).toBe("?q=a+b&r=%2Fapps");
  });

  it("leaves out undefined and returns nothing for no values", () => {
    expect(stringifySearch({ q: undefined, r: "1" })).toBe("?r=1");
    expect(stringifySearch({ q: undefined })).toBe("");
    expect(stringifySearch({})).toBe("");
  });

  it.each([
    ["a number", { account: 5 }],
    ["a boolean", { account: true }],
    ["null", { account: null }],
    ["an object", { account: {} }],
    ["an array with a number", { account: ["1", 2] }],
  ])("refuses %s and names the key", (_, search) => {
    expect(() => stringifySearch(search)).toThrow(TypeError);
    expect(() => stringifySearch(search)).toThrow(/"account"/);
  });

  it("is undone by parseSearch", () => {
    for (const searchStr of [
      "?q=123",
      "?ref=12e45678",
      "?ref=1e000000",
      "?q=1.0",
      "?q=-0",
      "?q=true",
      "?q=null",
      "?admin",
      "?admin=",
      "?admin=%221%22",
      "?a=1&b=2&a=3",
      "?q=a+b",
      "?q=a%20b",
      "?__proto__=x",
    ]) {
      const search = parseSearch(searchStr);
      expect(parseSearch(stringifySearch(search))).toEqual(search);
    }
  });
});

describe("search fields", () => {
  const parse = (schema: z.ZodType, value: unknown) =>
    z.object({ key: schema }).parse(value === undefined ? {} : { key: value })
      .key;

  it("searchText", () => {
    const field = searchText();
    expect(parse(field, undefined)).toBe("");
    expect(parse(field, "123")).toBe("123");
    expect(parse(field, "")).toBe("");
    expect(parse(field, ["a", "b"])).toBe("");
  });

  it("searchText with a format", () => {
    const field = searchText(z.string().regex(/^\d{4}$/));
    expect(parse(field, undefined)).toBe("");
    expect(parse(field, "2026")).toBe("2026");
    expect(parse(field, "26")).toBe("");
    expect(parse(field, ["2026"])).toBe("");
  });

  it("searchChoice", () => {
    const field = searchChoice(["a", "b"], "a");
    expect(parse(field, undefined)).toBe("a");
    expect(parse(field, "b")).toBe("b");
    expect(parse(field, "B")).toBe("a");
    expect(parse(field, " b ")).toBe("a");
    expect(parse(field, ["b", "b"])).toBe("a");
  });

  it("optionalSearchText", () => {
    const field = optionalSearchText(z.string().regex(/^\d+$/));
    const schema = z.object({ key: field });
    expect(schema.parse({})).toEqual({});
    expect(schema.parse({ key: "12" })).toEqual({ key: "12" });
    expect(schema.parse({ key: "x" })).toEqual({ key: undefined });
    expect(schema.parse({ key: ["1", "2"] })).toEqual({ key: undefined });
  });

  it("rawSearchValue", () => {
    const schema = z.object({ key: rawSearchValue });
    expect(schema.parse({})).toEqual({});
    expect(schema.parse({ key: "" })).toEqual({ key: "" });
    expect(schema.parse({ key: ["/a", "/b"] })).toEqual({ key: ["/a", "/b"] });
  });
});

describe("the router with this serialization", () => {
  function routerAt(path: string) {
    const rootRoute = createRootRoute();
    const route = createRoute({
      getParentRoute: () => rootRoute,
      path: "/x",
      validateSearch: z.object({ q: optionalSearchText() }),
    });
    return createRouter({
      ...searchSerialization,
      routeTree: rootRoute.addChildren([route]),
      history: createMemoryHistory({ initialEntries: [path] }),
    });
  }

  it("keeps the address as it came", async () => {
    const router = routerAt("/x?ref=12e45678&return_to=%2Fa&return_to=%2Fb");
    await router.load();
    const { location } = router.state;
    expect(location.search).toEqual({
      ref: "12e45678",
      return_to: ["/a", "/b"],
    });
    expect(location.href).toBe("/x?ref=12e45678&return_to=%2Fa&return_to=%2Fb");
  });

  it("writes a string that looks like a number without quotes", async () => {
    const router = routerAt("/x");
    await router.load();
    // Link options are typed against the app's registered routes, which do not
    // include `/x`.
    const options = { to: "/x", search: { q: "123" } } as never;
    expect(router.buildLocation(options).href).toBe("/x?q=123");
  });
});
