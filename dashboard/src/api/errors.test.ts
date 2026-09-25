import { describe, expect, it } from "vitest";
import { ApiError, describeError } from "@/api/errors";

function refused(code: string) {
  return new ApiError({ kind: "http", status: 404, code, requestId: "r1" });
}

describe("describeError", () => {
  it("reads a scoped wording before the general one", () => {
    // The signing-key handlers reuse the passkey's "not found" code.
    expect(describeError(refused("credential_not_found")).id).toBe(
      "error.credential_not_found",
    );
    expect(
      describeError(refused("credential_not_found"), "signing-key").id,
    ).toBe("error.signing-key.credential_not_found");
  });

  it("falls back to the general wording for a code the scope does not cover", () => {
    expect(
      describeError(refused("active_key_no_replacement"), "signing-key").id,
    ).toBe("error.active_key_no_replacement");
    expect(describeError(refused("mystery"), "signing-key")).toMatchObject({
      id: "error.request_failed",
      requestId: "r1",
    });
  });
});
