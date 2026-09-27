import { describe, expect, it } from "vitest";
import {
  absoluteHttpUrlProblem,
  addressInvalid,
  aliasListProblem,
  removedScopes,
  scopeFormValue,
  uriListProblem,
} from "@/pages/admin/oidc-applications/oidc-validation";

/**
 * These rules are the only check any of these values gets before a client
 * tries to sign in with them. The cases below are the "nearly right" ones: an
 * address pasted with a trailing space, the same callback twice, a record
 * saved before `openid` was required.
 */
describe("scope form value", () => {
  it("adds openid when the saved record lacks it", () => {
    expect(scopeFormValue(["profile", "email"])).toEqual([
      "openid",
      "profile",
      "email",
    ]);
    expect(scopeFormValue([])).toEqual(["openid"]);
  });

  it("drops scopes the instance does not know", () => {
    expect(scopeFormValue(["openid", "phone", "groups"])).toEqual([
      "openid",
      "groups",
    ]);
  });

  it("follows the vocabulary's order, not the saved one", () => {
    expect(scopeFormValue(["groups", "email", "openid"])).toEqual([
      "openid",
      "email",
      "groups",
    ]);
  });
});

describe("removed scopes", () => {
  it("names the saved scopes the new set lacks, in saved order", () => {
    expect(
      removedScopes(["openid", "groups", "email"], ["openid", "email"]),
    ).toEqual(["groups"]);
    expect(removedScopes(["openid"], ["openid", "profile"])).toEqual([]);
  });

  it("counts a saved scope the checkboxes never drew", () => {
    // "phone" is not in the vocabulary, so the form cannot hold it and the save
    // drops it; the confirmation has to name it.
    const saved = ["openid", "phone"];
    expect(removedScopes(saved, scopeFormValue(saved))).toEqual(["phone"]);
  });
});

describe("address", () => {
  it("accepts absolute http and https addresses, loopback included", () => {
    expect(
      absoluteHttpUrlProblem("https://app.example.com/callback"),
    ).toBeUndefined();
    expect(
      absoluteHttpUrlProblem("http://127.0.0.1:8400/callback"),
    ).toBeUndefined();
  });

  it("refuses a relative address, another scheme or credentials", () => {
    expect(absoluteHttpUrlProblem("/callback")).toBe(addressInvalid);
    expect(absoluteHttpUrlProblem("app://callback")).toBe(addressInvalid);
    expect(absoluteHttpUrlProblem("https://user:pw@app.example.com/")).toBe(
      addressInvalid,
    );
  });
});

describe("address list", () => {
  it("accepts distinct, usable addresses and an empty list", () => {
    expect(
      uriListProblem([
        "https://app.example.com/callback",
        "http://127.0.0.1:8400/callback",
      ]),
    ).toBeUndefined();
    expect(uriListProblem([])).toBeUndefined();
  });

  it("refuses an empty row", () => {
    expect(uriListProblem(["https://a.example/cb", ""])?.index).toBe(1);
    expect(uriListProblem(["   "])?.message.id).toBe(
      "admin.oidc-apps.uri.required",
    );
  });

  it("refuses spaces at either end rather than trimming them", () => {
    expect(uriListProblem([" https://a.example/cb"])?.message.id).toBe(
      "admin.oidc-apps.uri.spaces",
    );
    expect(uriListProblem(["https://a.example/cb "])?.message.id).toBe(
      "admin.oidc-apps.uri.spaces",
    );
  });

  it("refuses an address that is not absolute http or https", () => {
    expect(uriListProblem(["https://a.example/cb", "a.example/cb"])).toEqual({
      index: 1,
      message: addressInvalid,
    });
  });

  it("refuses the same address twice, naming the later row", () => {
    expect(
      uriListProblem([
        "https://a.example/cb",
        "https://b.example/cb",
        "https://a.example/cb",
      ]),
    ).toMatchObject({
      index: 2,
      message: { id: "admin.oidc-apps.uri.duplicate" },
    });
  });

  it("reports the first problem only", () => {
    expect(uriListProblem(["nope", ""])).toEqual({
      index: 0,
      message: addressInvalid,
    });
  });
});

describe("claim aliases", () => {
  const alias = (name: string) => ({ name, source: "name" });

  it("accepts identifier-shaped names", () => {
    expect(aliasListProblem([alias("team"), alias("_nick2")])).toBeUndefined();
  });

  it("refuses a name with spaces at either end rather than trimming it", () => {
    expect(aliasListProblem([alias(" team")])).toMatchObject({
      index: 0,
      field: "name",
      message: { id: "admin.oidc-apps.alias.name.invalid" },
    });
    expect(aliasListProblem([alias("team ")])?.message.id).toBe(
      "admin.oidc-apps.alias.name.invalid",
    );
  });

  it("refuses an empty, reserved, overlong or repeated name", () => {
    expect(aliasListProblem([alias("")])?.message.id).toBe(
      "admin.oidc-apps.alias.name.required",
    );
    expect(aliasListProblem([alias("sub")])?.message.id).toBe(
      "admin.oidc-apps.alias.name.reserved",
    );
    expect(aliasListProblem([alias("a".repeat(65))])?.message.id).toBe(
      "admin.oidc-apps.alias.name.invalid",
    );
    expect(aliasListProblem([alias("team"), alias("team")])).toMatchObject({
      index: 1,
      message: { id: "admin.oidc-apps.alias.name.duplicate" },
    });
  });
});
