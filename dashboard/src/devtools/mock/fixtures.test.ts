import { describe, expect, it } from "vitest";
import { isValidRecoveryCode } from "@/api/auth";
import { buildMockReply, type MockReply } from "@/devtools/mock/fixtures";
import {
  defaultMockConfig,
  type MockConfig,
  mockListMax,
} from "@/devtools/mock/model";

function config(overrides: (draft: MockConfig) => void = () => {}): MockConfig {
  const draft = structuredClone(defaultMockConfig);
  overrides(draft);
  return draft;
}

/** The default config with the write switch on. */
function writes(): MockConfig {
  return config((draft) => {
    draft.writes = true;
  });
}

function call(
  method: string,
  schemaPath: string,
  current: MockConfig = config(),
  body?: unknown,
): MockReply | undefined {
  return buildMockReply(
    { method, schemaPath, url: `http://localhost${schemaPath}`, body },
    current,
  );
}

function read(
  schemaPath: string,
  current: MockConfig = config(),
  url = `http://localhost${schemaPath}`,
): MockReply | undefined {
  return buildMockReply({ method: "GET", schemaPath, url }, current);
}

/** The config as a reply's effect leaves it, which is what the next read sees. */
function applied(
  reply: MockReply | undefined,
  current: MockConfig,
): MockConfig {
  const draft = structuredClone(current);
  reply?.effect?.(draft);
  return draft;
}

function bodyOf(reply: MockReply | undefined): unknown {
  if (reply?.kind !== "json")
    throw new Error(`not a JSON reply: ${reply?.kind}`);
  return reply.body;
}

describe("mock replies", () => {
  it("answers protected reads with no_session while the panel is signed out", () => {
    for (const path of [
      "/api/prohibitorum/me",
      "/api/prohibitorum/me/credentials",
      "/api/prohibitorum/me/factors",
      "/api/prohibitorum/me/sessions",
      "/api/prohibitorum/me/sudo/methods",
    ]) {
      expect(
        read(
          path,
          config((draft) => {
            draft.session.signedIn = false;
          }),
        ),
      ).toEqual({
        kind: "error",
        status: 401,
        code: "no_session",
      });
    }
  });

  it("serves the public reads regardless of the session", () => {
    const signedOut = config((draft) => {
      draft.session.signedIn = false;
      draft.instance.maintenance = true;
    });
    expect(bodyOf(read("/api/prohibitorum/config", signedOut))).toMatchObject({
      maintenanceMode: true,
    });
    expect(
      bodyOf(read("/api/prohibitorum/auth/federation", signedOut)),
    ).toBeInstanceOf(Array);
  });

  it("takes the account's name from the panel", () => {
    const current = config((draft) => {
      draft.session.displayName = "Ada";
      draft.session.username = "ada";
    });
    expect(bodyOf(read("/api/prohibitorum/me", current))).toMatchObject({
      displayName: "Ada",
      username: "ada",
    });
  });

  it("derives the factor counts from the panel", () => {
    const current = config((draft) => {
      draft.factors.passkeys = 3;
      draft.factors.recoveryCodes = 4;
      draft.factors.passwordSet = false;
      draft.factors.totpEnrolled = false;
    });
    expect(bodyOf(read("/api/prohibitorum/me/factors", current))).toEqual({
      passkeyCount: 3,
      recoveryCodesRemaining: 4,
      passwordSet: false,
      totpEnrolled: false,
    });
    expect(
      bodyOf(read("/api/prohibitorum/me/credentials", current)),
    ).toHaveLength(3);
  });

  it("marks the first listed session as the current one", () => {
    const current = config((draft) => {
      draft.lists.sessions = 3;
    });
    const sessions = bodyOf(
      read("/api/prohibitorum/me/sessions", current),
    ) as Array<{
      isCurrent: boolean;
    }>;
    expect(sessions.map((session) => session.isCurrent)).toEqual([
      true,
      false,
      false,
    ]);
  });

  it("caps every list at the panel's maximum", () => {
    const current = config((draft) => {
      draft.lists.identities = mockListMax + 5;
      draft.lists.tokens = 0;
    });
    expect(
      bodyOf(read("/api/prohibitorum/me/identities", current)),
    ).toHaveLength(mockListMax);
    expect(bodyOf(read("/api/prohibitorum/me/tokens", current))).toEqual([]);
  });

  it("offers only the step-up methods the panel allows", () => {
    const current = config((draft) => {
      draft.sudo.webauthn = false;
      draft.sudo.fresh = false;
    });
    expect(bodyOf(read("/api/prohibitorum/me/sudo/methods", current))).toEqual({
      methods: ["password_totp"],
      fresh: false,
    });
  });

  it("echoes the code a device pairing lookup was asked for", () => {
    const reply = read(
      "/api/prohibitorum/me/devices/pair/lookup",
      config(),
      "http://localhost/api/prohibitorum/me/devices/pair/lookup?code=ABCD2345",
    );
    expect(bodyOf(reply)).toMatchObject({ displayCode: "ABCD2345" });
  });

  it("leaves an endpoint it does not serve to the server", () => {
    expect(read("/api/prohibitorum/accounts")).toBeUndefined();
  });
});

describe("mocked writes", () => {
  it("leaves a write to the server until the switch is on", () => {
    expect(call("POST", "/api/prohibitorum/auth/logout")).toBeUndefined();
    expect(
      call("POST", "/api/prohibitorum/auth/logout", writes()),
    ).toMatchObject({ kind: "empty", status: 204 });
  });

  it("signs the panel out on logout, so the next read is anonymous", () => {
    const current = writes();
    const reply = call("POST", "/api/prohibitorum/auth/logout", current);
    expect(reply).toMatchObject({ kind: "empty", status: 204 });
    const signedOut = applied(reply, current);

    expect(read("/api/prohibitorum/me", signedOut)).toEqual({
      kind: "error",
      status: 401,
      code: "no_session",
    });
  });

  it("applies the new display name the profile write carried", () => {
    const current = writes();
    const reply = call("PUT", "/api/prohibitorum/me", current, {
      displayName: "Ada Lovelace",
    });
    expect(bodyOf(reply)).toMatchObject({
      displayName: "Ada Lovelace",
      username: current.session.username,
    });
    expect(applied(reply, current).session.displayName).toBe("Ada Lovelace");
  });

  it("shrinks the passkey list the next read returns", () => {
    const current = writes();
    current.factors.passkeys = 3;
    const reply = call(
      "POST",
      "/api/prohibitorum/me/credentials/delete",
      current,
      {
        id: 2,
      },
    );
    expect(reply).toMatchObject({ kind: "empty", status: 204 });
    expect(
      bodyOf(read("/api/prohibitorum/me/credentials", applied(reply, current))),
    ).toHaveLength(2);
  });

  it("hands out recovery codes in the shape the console accepts", () => {
    const current = writes();
    const reply = call("POST", "/api/prohibitorum/me/totp/verify", current, {
      secret_base32: "MOCK",
      code: "123456",
    });
    const codes = (bodyOf(reply) as { recovery_codes: string[] })
      .recovery_codes;
    expect(codes).toHaveLength(10);
    expect(codes.every(isValidRecoveryCode)).toBe(true);

    expect(
      bodyOf(read("/api/prohibitorum/me/factors", applied(reply, current))),
    ).toMatchObject({ totpEnrolled: true, recoveryCodesRemaining: 10 });
  });

  it("returns a created token and grows the token list", () => {
    const current = writes();
    current.lists.tokens = 1;
    const reply = call("POST", "/api/prohibitorum/me/tokens", current, {
      name: "CI",
      allApps: false,
      appGrants: { "mock-client-1": ["openid"] },
    });
    expect(bodyOf(reply)).toMatchObject({
      pat: { name: "CI", allApps: false },
    });
    expect((bodyOf(reply) as { token: string }).token).toMatch(/^phb_mock_/);
    expect(
      bodyOf(read("/api/prohibitorum/me/tokens", applied(reply, current))),
    ).toHaveLength(2);
  });

  it("answers a password step-up and leaves the passkey one to the server", () => {
    const current = writes();
    expect(
      call("POST", "/api/prohibitorum/me/sudo/begin", current, {
        method: "webauthn",
      }),
    ).toBeUndefined();
    expect(
      call("POST", "/api/prohibitorum/me/sudo/begin", current, {
        method: "password_totp",
      }),
    ).toMatchObject({ kind: "empty" });

    const complete = call(
      "POST",
      "/api/prohibitorum/me/sudo/complete",
      current,
    );
    expect(applied(complete, current).sudo.fresh).toBe(true);
  });
});
