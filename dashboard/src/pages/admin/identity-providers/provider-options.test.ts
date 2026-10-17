import { describe, expect, it } from "vitest";
import {
  documentSourceLabel,
  identityFields,
  identityValue,
  providerModesForSignIn,
} from "@/pages/admin/identity-providers/provider-options";

describe("identityFields", () => {
  it("lists the seven mapped fields in reading order", () => {
    expect(identityFields.map((field) => field.key)).toEqual([
      "issuer",
      "subject",
      "username",
      "displayName",
      "email",
      "emailVerified",
      "picture",
    ]);
  });
});

describe("identityValue", () => {
  it("reads text, booleans and absent values", () => {
    expect(identityValue({ value: "alice", source: "id_token" })).toEqual({
      kind: "text",
      text: "alice",
    });
    expect(identityValue({ value: false, source: "userinfo" })).toEqual({
      kind: "boolean",
      value: false,
    });
    expect(identityValue({ value: null, source: "userinfo" })).toEqual({
      kind: "missing",
    });
    expect(identityValue({ value: "", source: "id_token" })).toEqual({
      kind: "missing",
    });
  });
});

describe("documentSourceLabel", () => {
  it("names the three sources and nothing else", () => {
    expect(documentSourceLabel("id_token")?.id).toBe(
      "admin.federation.diagnostics.stage.id-token",
    );
    expect(documentSourceLabel("userinfo")?.id).toBe(
      "admin.federation.diagnostics.stage.userinfo",
    );
    expect(documentSourceLabel("configuration")?.id).toBe(
      "admin.federation.diagnostics.source.configuration",
    );
    expect(documentSourceLabel("discovery")).toBeUndefined();
  });
});

describe("providerModesForSignIn", () => {
  it("offers every mode while sign-in is on", () => {
    expect(providerModesForSignIn(true).map((o) => o.value)).toEqual([
      "auto_provision",
      "invite_only",
      "link_only",
    ]);
  });

  it("drops auto_provision while sign-in is off", () => {
    expect(providerModesForSignIn(false).map((o) => o.value)).toEqual([
      "invite_only",
      "link_only",
    ]);
  });
});
