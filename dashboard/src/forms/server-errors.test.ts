import { describe, expect, it } from "vitest";
import { ApiError } from "@/api/errors";
import {
  mapServerError,
  ServerFormError,
  withoutServerErrors,
} from "@/forms/server-errors";

const mapping = {
  locations: { "body.nickname": "nickname" },
  codes: { invalid_nickname: "nickname" },
} as const;

function validationError(location: string) {
  return new ApiError({
    kind: "http",
    status: 422,
    code: "validation_failed",
    details: { location, reason: "Untrusted server text" },
  });
}

describe("server form errors", () => {
  it("maps only explicit locations and business codes to mounted fields", () => {
    expect(
      mapServerError(validationError("body.nickname"), mapping, ["nickname"]),
    ).toMatchObject({ fields: { nickname: expect.any(ServerFormError) } });
    expect(
      mapServerError(
        new ApiError({ kind: "http", code: "invalid_nickname" }),
        mapping,
        ["nickname"],
      ),
    ).toMatchObject({ fields: { nickname: expect.any(ServerFormError) } });
    expect(
      mapServerError(validationError("body.nickname"), mapping, []),
    ).toMatchObject({ form: expect.any(ServerFormError) });
  });

  it.each(["query.nickname", "__proto__"])(
    "keeps unregistered location %s in the summary",
    (location) => {
      const error = mapServerError(validationError(location), mapping, [
        "nickname",
      ]);
      expect(error.fields).toBeUndefined();
      expect(error.form).toBeInstanceOf(ServerFormError);
      expect(error.form?.message).not.toHaveProperty("reason");
    },
  );

  it("does not infer a field from unknown codes or non-validation details", () => {
    const error = new ApiError({
      kind: "http",
      code: "invalid_group_rule",
      details: { location: "body.nickname", path: "nickname" },
    });
    expect(mapServerError(error, mapping, ["nickname"])).toMatchObject({
      form: expect.any(ServerFormError),
    });
  });

  it("removes only server errors from a mixed submission error", () => {
    const client = { id: "client.validation", message: "Client validation" };
    const server = new ServerFormError(validationError("body.nickname"));
    expect(withoutServerErrors([client, server])).toEqual([client]);
    expect(withoutServerErrors(client)).toBe(client);
    expect(withoutServerErrors([server])).toBeUndefined();
  });
});
