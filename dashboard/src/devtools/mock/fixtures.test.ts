import { describe, expect, it } from "vitest";
import { isValidRecoveryCode } from "@/api/auth";
import { buildMockReply, type MockReply } from "@/devtools/mock/fixtures";
import {
  defaultMockConfig,
  type MockConfig,
  mockListMax,
  mockPageSize,
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

/**
 * One request, as the client sends it. A `{placeholder}` in the schema path is
 * replaced by the matching body field, because that is what openapi-fetch does
 * before the request leaves: the fixture reads an id from the URL, never from
 * the template it was registered under.
 */
function call(
  method: string,
  schemaPath: string,
  current: MockConfig = config(),
  body?: unknown,
): MockReply | undefined {
  const path = schemaPath.replace(/\{(\w+)\}/g, (_, key: string) =>
    String((body as Record<string, unknown> | undefined)?.[key] ?? key),
  );
  return buildMockReply(
    { method, schemaPath, url: `http://localhost${path}`, body },
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

  it("fails an endpoint it has no fixture for instead of reaching the server", () => {
    expect(read("/api/prohibitorum/oidc-applications")).toEqual({
      kind: "error",
      status: 501,
      code: "mock_unmocked",
      details: { method: "GET", path: "/api/prohibitorum/oidc-applications" },
    });
  });
});

describe("mocked management directory", () => {
  it("serves the account directory one cursor page at a time", () => {
    const current = config((draft) => {
      draft.admin.accounts = mockPageSize + 1;
    });
    const first = bodyOf(read("/api/prohibitorum/accounts", current)) as {
      items: unknown[];
      nextCursor: string;
    };
    expect(first.items).toHaveLength(mockPageSize);
    expect(first.nextCursor).toBe(String(mockPageSize));

    const second = bodyOf(
      read(
        "/api/prohibitorum/accounts",
        current,
        `http://localhost/api/prohibitorum/accounts?cursor=${first.nextCursor}`,
      ),
    ) as { items: unknown[]; nextCursor: string };
    expect(second.items).toHaveLength(1);
    expect(second.nextCursor).toBe("");
  });

  it("signs the panel in as the account the directory lists first", () => {
    const current = config((draft) => {
      draft.session.username = "ada";
      draft.session.role = "member";
    });
    const page = bodyOf(read("/api/prohibitorum/accounts", current)) as {
      items: Array<{ username: string; role: string }>;
    };
    expect(page.items[0]).toMatchObject({ username: "ada", role: "member" });
  });

  it("answers an unknown account with not_found rather than a fabricated one", () => {
    const many = config((draft) => {
      draft.admin.accounts = 2;
    });
    expect(
      read(
        "/api/prohibitorum/accounts/{id}",
        many,
        "http://localhost/api/prohibitorum/accounts/99",
      ),
    ).toEqual({ kind: "error", status: 404, code: "not_found" });
  });

  it("offers both a manual and a rule group, so both edit shapes are reachable", () => {
    const groups = bodyOf(read("/api/prohibitorum/groups", config())) as Array<{
      kind: string;
      rule?: unknown;
    }>;
    expect(groups.map((group) => group.kind)).toEqual([
      "manual",
      "rule",
      "manual",
    ]);
    expect(groups[1]?.rule).toBeDefined();
    expect(groups[0]?.rule).toBeUndefined();
  });

  it("still lists a group when the panel is set to none", () => {
    const current = config((draft) => {
      draft.admin.groups = 0;
    });
    expect(bodyOf(read("/api/prohibitorum/groups", current))).toHaveLength(1);
  });

  it("pages invitations and names their upstream provider", () => {
    const invitations = bodyOf(
      read(
        "/api/prohibitorum/invitations",
        config((draft) => {
          draft.admin.invitations = 2;
        }),
      ),
    ) as { items: Array<{ expectedUpstreamIdpSlug?: string }> };
    expect(invitations.items).toHaveLength(2);
    expect(invitations.items[0]?.expectedUpstreamIdpSlug).toBe("provider-1");
    expect(invitations.items[1]?.expectedUpstreamIdpSlug).toBeUndefined();
  });

  it("publishes the search fields and operators the advanced filter reads", () => {
    const page = bodyOf(
      read("/api/prohibitorum/identity-providers", config()),
    ) as {
      items: Array<{
        slug: string;
        searchFields: Array<{ operators: string[] }>;
      }>;
      nextCursor: string;
    };
    expect(page.items[0]?.searchFields).toEqual([
      { key: "email", operators: ["eq", "contains"] },
      { key: "subject", operators: ["eq"] },
    ]);
  });

  it("matches nobody for a rule draft that carries no condition", () => {
    const current = writes();
    const draft = call(
      "POST",
      "/api/prohibitorum/groups/rule-preview",
      current,
      {
        version: 1,
        condition: { op: "all", children: [] },
      },
    );
    expect(bodyOf(draft)).toMatchObject({ items: [], matchedCount: 0 });
  });

  it("predicts matches for a rule draft that carries a condition", () => {
    const current = writes();
    const reply = call(
      "POST",
      "/api/prohibitorum/groups/rule-preview",
      current,
      {
        version: 1,
        condition: { fact: "login_method", method: "passkey" },
      },
    );
    const preview = bodyOf(reply) as { items: unknown[]; matchedCount: number };
    expect(preview.matchedCount).toBeGreaterThan(0);
  });

  it("shrinks the directory a create or delete left behind", () => {
    const current = writes();
    current.admin.accounts = 3;
    const deleted = call("POST", "/api/prohibitorum/accounts/delete", current, {
      id: 2,
    });
    expect(
      bodyOf(read("/api/prohibitorum/accounts", applied(deleted, current))),
    ).toMatchObject({ items: expect.any(Array) });
    expect(applied(deleted, current).admin.accounts).toBe(2);

    const created = call("POST", "/api/prohibitorum/invitations", current, {
      role: "member",
    });
    expect(bodyOf(created)).toMatchObject({
      url: expect.stringContaining("/enroll/"),
    });
    const revoked = call(
      "POST",
      "/api/prohibitorum/invitations/revoke",
      applied(created, current),
      { token: "mock-invitation-1" },
    );
    expect(applied(revoked, applied(created, current)).admin.invitations).toBe(
      current.admin.invitations,
    );
  });

  it("carries a role change back onto the signed-in session", () => {
    const current = writes();
    const reply = call("PUT", "/api/prohibitorum/accounts/{id}", current, {
      id: 1,
      username: current.session.username,
      displayName: current.session.displayName,
      role: "member",
    });
    expect(applied(reply, current).session.role).toBe("member");
    expect(
      bodyOf(read("/api/prohibitorum/me", applied(reply, current))),
    ).toMatchObject({ role: "member" });
  });
});

describe("mocked logs, settings and signing keys", () => {
  type Page<T> = { items: T[]; nextCursor: string };
  type Event = {
    factor: string;
    event: string;
    at: string;
    accountId?: number;
    accountUsername?: string;
  };
  type Key = { kid: string; status: string };

  it("pages the audit log and applies the server's filters", () => {
    const current = config((draft) => {
      draft.admin.auditEvents = 24;
    });
    const first = bodyOf(
      read("/api/prohibitorum/audit-events", current),
    ) as Page<Event>;
    expect(first.items).toHaveLength(mockPageSize);
    expect(first.nextCursor).toBe(String(mockPageSize));
    // Some events belong to an account, named; some to nobody.
    const all = bodyOf(
      read(
        "/api/prohibitorum/audit-events",
        current,
        "http://localhost/api/prohibitorum/audit-events?cursor=0",
      ),
    ) as Page<Event>;
    expect(all.items.some((item) => item.accountUsername)).toBe(true);
    expect(all.items.some((item) => item.accountId === undefined)).toBe(true);

    const failed = bodyOf(
      read(
        "/api/prohibitorum/audit-events",
        current,
        "http://localhost/api/prohibitorum/audit-events?factor=password&event=fail",
      ),
    ) as Page<Event>;
    expect(failed.items.length).toBeGreaterThan(0);
    expect(
      failed.items.every(
        (item) => item.factor === "password" && item.event === "fail",
      ),
    ).toBe(true);

    const since = new Date(Date.now() - 3 * 3_600_000).toISOString();
    const recent = bodyOf(
      read(
        "/api/prohibitorum/audit-events",
        current,
        `http://localhost/api/prohibitorum/audit-events?since=${encodeURIComponent(since)}`,
      ),
    ) as Page<Event>;
    expect(recent.items).toHaveLength(2);
    expect(recent.nextCursor).toBe("");
  });

  it("carries a saved name, notice and images into /config", () => {
    let current = writes();
    for (const [method, path, body] of [
      ["PUT", "/api/prohibitorum/admin/settings", { instanceName: "Home" }],
      [
        "PUT",
        "/api/prohibitorum/admin/settings/maintenance",
        { maintenanceMode: true, maintenanceMessage: "Back soon" },
      ],
      ["PUT", "/api/prohibitorum/admin/settings/icon", undefined],
      ["PUT", "/api/prohibitorum/admin/settings/background", undefined],
    ] as const) {
      current = applied(call(method, path, current, body), current);
    }
    const saved = bodyOf(read("/api/prohibitorum/config", current)) as {
      instanceName: string;
      maintenanceMode: boolean;
      maintenanceMessage: string;
      hasCustomIcon: boolean;
      hasCustomBackground: boolean;
      iconEtag: string;
    };
    expect(saved).toMatchObject({
      instanceName: "Home",
      maintenanceMode: true,
      maintenanceMessage: "Back soon",
      hasCustomIcon: true,
      hasCustomBackground: true,
    });

    // Clearing the override falls back to the configured name, and removing
    // the icon is a new version of it.
    const before = saved.iconEtag;
    current = applied(
      call("PUT", "/api/prohibitorum/admin/settings", current, {
        instanceName: "",
      }),
      current,
    );
    current = applied(
      call("DELETE", "/api/prohibitorum/admin/settings/icon", current),
      current,
    );
    const cleared = bodyOf(read("/api/prohibitorum/config", current)) as {
      instanceName: string;
      hasCustomIcon: boolean;
      iconEtag: string;
    };
    expect(cleared.instanceName).toBe("Prohibitorum (mock)");
    expect(cleared.hasCustomIcon).toBe(false);
    expect(cleared.iconEtag).not.toBe(before);
  });

  it("stores the client-IP policy it was sent", () => {
    const policy = {
      strategy: "forwarded",
      header: "",
      trustedProxies: ["10.0.0.0/8", "2001:db8::/32"],
    };
    const current = applied(
      call(
        "PUT",
        "/api/prohibitorum/admin/settings/client-ip",
        writes(),
        policy,
      ),
      writes(),
    );
    expect(
      bodyOf(read("/api/prohibitorum/admin/settings/client-ip", current)),
    ).toEqual(policy);
  });

  it("moves signing keys through their states as the handlers do", () => {
    let current = writes();
    const keys = () =>
      (bodyOf(read("/api/prohibitorum/signing-keys", current)) as Page<Key>)
        .items;
    expect(keys().map((key) => key.status)).toEqual([
      "pending",
      "active",
      "decommissioning",
      "retired",
    ]);

    // A new key is pending and on top; the others keep their ids.
    const before = keys().map((key) => key.kid);
    const generated = call(
      "POST",
      "/api/prohibitorum/signing-keys/generate",
      current,
      {},
    );
    expect(generated).toMatchObject({ kind: "json", status: 201 });
    current = applied(generated, current);
    expect(
      keys()
        .slice(1)
        .map((key) => key.kid),
    ).toEqual(before);
    expect(keys()[0]?.status).toBe("pending");

    // Activating one pending key retires the signer.
    const target = keys()[1]?.kid ?? "";
    current = applied(
      call("POST", "/api/prohibitorum/signing-keys/{kid}/activate", current, {
        kid: target,
      }),
      current,
    );
    expect(keys().map((key) => key.status)).toEqual([
      "pending",
      "active",
      "decommissioning",
      "decommissioning",
      "retired",
    ]);

    // The signer cannot be retired, and a key that is not pending cannot be
    // activated.
    expect(
      call("POST", "/api/prohibitorum/signing-keys/{kid}/retire", current, {
        kid: target,
      }),
    ).toMatchObject({ code: "active_key_no_replacement", status: 409 });
    expect(
      call("POST", "/api/prohibitorum/signing-keys/{kid}/activate", current, {
        kid: target,
      }),
    ).toMatchObject({ code: "credential_not_found", status: 404 });

    current = applied(
      call("POST", "/api/prohibitorum/signing-keys/{kid}/retire", current, {
        kid: keys()[0]?.kid ?? "",
      }),
      current,
    );
    expect(keys()[0]?.status).toBe("decommissioning");
  });
});

describe("mocked writes", () => {
  it("leaves a write to the server until the switch is on", () => {
    expect(call("POST", "/api/prohibitorum/auth/logout")).toBeUndefined();
    expect(
      call("POST", "/api/prohibitorum/auth/logout", writes()),
    ).toMatchObject({ kind: "empty", status: 204 });
  });

  it("fails a write it has no fixture for once writes are on", () => {
    const current = writes();
    for (const [method, path] of [
      ["POST", "/api/prohibitorum/accounts"],
      ["DELETE", "/api/prohibitorum/me"],
    ] as const) {
      expect(call(method, path, current)).toEqual({
        kind: "error",
        status: 501,
        code: "mock_unmocked",
        details: { method, path },
      });
    }
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

  it("answers a password step-up and fails the passkey one it cannot fake", () => {
    const current = writes();
    expect(
      call("POST", "/api/prohibitorum/me/sudo/begin", current, {
        method: "webauthn",
      }),
    ).toEqual({
      kind: "error",
      status: 501,
      code: "mock_unmocked",
      details: { method: "POST", path: "/api/prohibitorum/me/sudo/begin" },
    });
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
