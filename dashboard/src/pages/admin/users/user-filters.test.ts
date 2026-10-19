import { describe, expect, it } from "vitest";
import { parseSearch } from "@/app/search-params";
import {
  accountFilterQuery,
  advancedFilterIncomplete,
  filtersSet,
  identityFilterSet,
  isMatchOperator,
  userSearch,
} from "@/pages/admin/users/user-filters";

/** The filters a page at `?searchStr` reads. */
function userFilters(searchStr: string) {
  return userSearch.parse(parseSearch(searchStr));
}

describe("userSearch", () => {
  it("keeps the values it was given", () => {
    expect(
      userFilters(
        "?q=ali&provider=github&field=email&value=%40example.com&match=prefix",
      ),
    ).toEqual({
      q: "ali",
      provider: "github",
      field: "email",
      value: "@example.com",
      match: "prefix",
      role: "",
      state: "",
    });
  });

  it("falls back to empty rather than guessing", () => {
    expect(userFilters("")).toEqual({
      q: "",
      provider: "",
      field: "",
      value: "",
      match: "",
      role: "",
      state: "",
    });
  });

  it("rejects a match operator outside the server's set", () => {
    // `fuzzy` is not one of exact/prefix/contains; the server would reject the
    // request, so the filter is dropped instead of sent.
    expect(userFilters("?match=fuzzy").match).toBe("");
  });

  it("does not trim or fold case", () => {
    expect(userFilters("?q=+Ali+").q).toBe(" Ali ");
    expect(userFilters("?match=Prefix").match).toBe("");
  });

  it("keeps only the two roles the server defines", () => {
    expect(userFilters("?role=admin").role).toBe("admin");
    expect(userFilters("?role=superuser").role).toBe("");
  });

  it("keeps a search that looks like a number as text", () => {
    expect(userFilters("?q=123").q).toBe("123");
    expect(userFilters("?q=12e45678").q).toBe("12e45678");
  });

  it("falls back to empty for a repeated value", () => {
    expect(userFilters("?role=admin&role=user").role).toBe("");
    expect(userFilters("?q=a&q=b").q).toBe("");
  });
});

describe("accountFilterQuery", () => {
  const empty = userFilters("");

  it("sends nothing when nothing was asked for", () => {
    expect(accountFilterQuery(empty)).toEqual({});
  });

  it("sends the free-text search alone", () => {
    expect(accountFilterQuery({ ...empty, q: "ali" })).toEqual({ q: "ali" });
  });

  it("sends a complete advanced filter whole", () => {
    expect(
      accountFilterQuery({
        ...empty,
        provider: "github",
        field: "email",
        value: "@example.com",
        match: "contains",
      }),
    ).toEqual({
      provider: "github",
      field: "email",
      value: "@example.com",
      match: "contains",
    });
  });

  it("withholds a partly filled advanced filter", () => {
    // The server rejects a partial one; sending it would turn a half-typed
    // filter into an error instead of a result.
    expect(
      accountFilterQuery({ ...empty, provider: "github", field: "email" }),
    ).toEqual({});
  });

  it("never sends role or state, which the endpoint does not accept", () => {
    expect(
      accountFilterQuery({ ...empty, role: "admin", state: "disabled" }),
    ).toEqual({});
  });
});

describe("advancedFilterIncomplete", () => {
  const empty = userFilters("");

  it("is false when the filter is untouched", () => {
    expect(advancedFilterIncomplete(empty)).toBe(false);
  });

  it("is false when every part is present", () => {
    expect(
      advancedFilterIncomplete({
        ...empty,
        provider: "github",
        field: "email",
        value: "@example.com",
        match: "exact",
      }),
    ).toBe(false);
  });

  it("is true for any partial fill", () => {
    expect(advancedFilterIncomplete({ ...empty, provider: "github" })).toBe(
      true,
    );
    expect(advancedFilterIncomplete({ ...empty, value: "x" })).toBe(true);
  });
});

describe("identityFilterSet and filtersSet", () => {
  const empty = userFilters("");

  it("see nothing set on the untouched filters", () => {
    expect(identityFilterSet(empty)).toBe(false);
    expect(filtersSet(empty)).toBe(false);
  });

  it("count a half-filled identity filter, which is still the reader's filter", () => {
    expect(identityFilterSet({ ...empty, provider: "github" })).toBe(true);
    expect(identityFilterSet({ ...empty, value: "x" })).toBe(true);
    expect(filtersSet({ ...empty, provider: "github" })).toBe(true);
  });

  it("count the search alone as a filter, but not as an identity filter", () => {
    expect(identityFilterSet({ ...empty, q: "ali" })).toBe(false);
    expect(filtersSet({ ...empty, q: "ali" })).toBe(true);
  });

  it("do not count role or state, which the page has no control for", () => {
    expect(filtersSet({ ...empty, role: "admin", state: "disabled" })).toBe(
      false,
    );
  });
});

describe("isMatchOperator", () => {
  it("accepts only the operators the server defines", () => {
    expect(isMatchOperator("exact")).toBe(true);
    expect(isMatchOperator("prefix")).toBe(true);
    expect(isMatchOperator("contains")).toBe(true);
    expect(isMatchOperator("eq")).toBe(false);
    expect(isMatchOperator("")).toBe(false);
  });
});
