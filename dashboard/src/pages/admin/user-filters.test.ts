import { describe, expect, it } from "vitest";
import {
  accountFilterQuery,
  advancedFilterIncomplete,
  userFilters,
} from "@/pages/admin/user-filters";

describe("userFilters", () => {
  it("keeps the values it was given", () => {
    expect(
      userFilters({
        q: "ali",
        provider: "github",
        field: "email",
        value: "@example.com",
        match: "prefix",
      }),
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
    expect(userFilters({})).toEqual({
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
    expect(userFilters({ match: "fuzzy" }).match).toBe("");
  });

  it("does not trim or fold case", () => {
    expect(userFilters({ q: " Ali " }).q).toBe(" Ali ");
    expect(userFilters({ match: "Prefix" }).match).toBe("");
  });

  it("keeps only the two roles the server defines", () => {
    expect(userFilters({ role: "admin" }).role).toBe("admin");
    expect(userFilters({ role: "superuser" }).role).toBe("");
  });

  it("ignores non-string values", () => {
    expect(userFilters({ q: 42, role: ["admin"] }).q).toBe("");
  });
});

describe("accountFilterQuery", () => {
  const empty = userFilters({});

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
  const empty = userFilters({});

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
