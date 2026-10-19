import { describe, expect, it } from "vitest";
import {
  certificateState,
  expiryState,
  signingKeyExpiry,
} from "@/pages/admin/saml-applications/saml-certificate";
import { samlAppFixture } from "@/test/saml-app";

const now = new Date("2026-10-19T00:00:00Z");

describe("the signing certificate", () => {
  it("says there is none when no signing key is published", () => {
    expect(certificateState(samlAppFixture({ keys: [] }), now)).toEqual({
      kind: "none",
    });
    expect(
      certificateState(
        samlAppFixture({
          keys: [{ use: "encryption", notAfter: "2031-01-01T00:00:00Z" }],
        }),
        now,
      ),
    ).toEqual({ kind: "none" });
  });

  it("reads an expiry in the future as valid, with its date", () => {
    expect(
      certificateState(
        samlAppFixture({
          keys: [{ use: "signing", notAfter: "2027-01-01T00:00:00Z" }],
        }),
        now,
      ),
    ).toEqual({ kind: "valid", notAfter: "2027-01-01T00:00:00Z" });
  });

  it("reads an expiry in the past as expired", () => {
    expect(
      certificateState(
        samlAppFixture({
          keys: [{ use: "signing", notAfter: "2026-01-01T00:00:00Z" }],
        }),
        now,
      ),
    ).toEqual({ kind: "expired", notAfter: "2026-01-01T00:00:00Z" });
  });

  it("takes the earliest expiry among the signing keys", () => {
    const app = samlAppFixture({
      keys: [
        { use: "signing", notAfter: "2029-01-01T00:00:00Z" },
        { use: "encryption", notAfter: "2020-01-01T00:00:00Z" },
        { use: "signing", notAfter: "2027-06-01T00:00:00Z" },
      ],
    });
    expect(signingKeyExpiry(app)).toBe("2027-06-01T00:00:00Z");
  });

  it("does not call a key without an expiry date expired", () => {
    expect(
      certificateState(samlAppFixture({ keys: [{ use: "signing" }] }), now),
    ).toEqual({ kind: "no-expiry" });
  });

  it("does not call an expiry that is not a date valid or expired", () => {
    expect(expiryState("not-a-date", now)).toEqual({ kind: "unreadable" });
    expect(
      certificateState(
        samlAppFixture({ keys: [{ use: "signing", notAfter: "garbage" }] }),
        now,
      ),
    ).toEqual({ kind: "unreadable" });
  });
});
