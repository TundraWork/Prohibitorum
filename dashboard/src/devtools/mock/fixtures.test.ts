import { describe, expect, it } from "vitest";
import { isValidRecoveryCode } from "@/api/auth";
import { readEnrollmentPreview } from "@/api/enrollment";
import { readFederationFlow } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import type { DiagnosticResultView } from "@/api/raw-admin-paths";
import type {
  ConsentRequest,
  DevicePairing,
  FederationConfirm,
  SamlConsentRequest,
} from "@/api/raw-paths";
import { buildMockReply, type MockReply } from "@/devtools/mock/fixtures";
import {
  defaultMockConfig,
  type MockConfig,
  mockListMax,
  mockPageSize,
} from "@/devtools/mock/model";

type SessionListItem = components["schemas"]["SessionListItem"];

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

  it("answers a pairing lookup as the panel sets it up", () => {
    const url =
      "http://localhost/api/prohibitorum/me/devices/pair/lookup?code=ABCD2345";
    const plain = bodyOf(
      read("/api/prohibitorum/me/devices/pair/lookup", config(), url),
    ) as DevicePairing;
    const sessions = bodyOf(
      read("/api/prohibitorum/me/sessions", config()),
    ) as SessionListItem[];
    const current = sessions.find((session) => session.isCurrent);
    expect(plain.alreadyBound).toBe(false);
    expect(plain.initiatorIp).not.toBe(current?.lastSeenIp);
    // A real User-Agent, so the console has something to read a name from.
    expect(plain.initiatorUa).toContain("Chrome/");
    const lifetime = Date.parse(plain.expiresAt) - Date.now();
    expect(lifetime).toBeGreaterThan(290_000);
    expect(lifetime).toBeLessThanOrEqual(300_000);

    const set = bodyOf(
      read(
        "/api/prohibitorum/me/devices/pair/lookup",
        config((draft) => {
          draft.pairing = {
            expiresInSeconds: 10,
            alreadyBound: true,
            sameNetwork: true,
          };
        }),
        url,
      ),
    ) as DevicePairing;
    expect(set.alreadyBound).toBe(true);
    expect(set.initiatorIp).toBe(current?.lastSeenIp);
    expect(Date.parse(set.expiresAt) - Date.now()).toBeLessThanOrEqual(10_000);
  });

  it("reports sessions from real browsers", () => {
    const sessions = bodyOf(
      read(
        "/api/prohibitorum/me/sessions",
        config((draft) => {
          draft.lists.sessions = 3;
        }),
      ),
    ) as SessionListItem[];
    expect(sessions.map((session) => session.userAgent)).toEqual([
      expect.stringContaining("Firefox/"),
      expect.stringContaining("iPhone"),
      expect.stringContaining("Windows NT"),
    ]);
  });

  it("fails an endpoint it has no fixture for instead of reaching the server", () => {
    // The account's own application list has no fixture on purpose: nothing in
    // the console reads it, so a walkthrough never reaches it.
    expect(read("/api/prohibitorum/me/apps")).toEqual({
      kind: "error",
      status: 501,
      code: "mock_unmocked",
      details: { method: "GET", path: "/api/prohibitorum/me/apps" },
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

  it("searches the account directory by name before it pages", () => {
    const current = config((draft) => {
      draft.admin.accounts = mockPageSize * 2 + 12;
    });
    const found = bodyOf(
      read(
        "/api/prohibitorum/accounts",
        current,
        "http://localhost/api/prohibitorum/accounts?q=MOCK-USER-12",
      ),
    ) as { items: Array<{ id: number }>; nextCursor: string };
    expect(found.items.map((account) => account.id)).toEqual([12]);
    expect(found.nextCursor).toBe("");

    // The rows carry timestamps relative to now, so the pages are compared by
    // which accounts they hold.
    const page = (url: string) => {
      const body = bodyOf(read("/api/prohibitorum/accounts", current, url)) as {
        items: Array<{ id: number }>;
        nextCursor: string;
      };
      return [body.items.map((account) => account.id), body.nextCursor];
    };
    expect(page("http://localhost/api/prohibitorum/accounts?q=")).toEqual(
      page("http://localhost/api/prohibitorum/accounts"),
    );
  });

  it("lists accounts 2 and 3 as the managers of every kind of application", () => {
    for (const [schemaPath, url] of [
      [
        "/api/prohibitorum/oidc-applications/{clientId}/managers",
        "http://localhost/api/prohibitorum/oidc-applications/oidc-client-1/managers",
      ],
      [
        "/api/prohibitorum/forward-auth-apps/{clientId}/managers",
        "http://localhost/api/prohibitorum/forward-auth-apps/fa-client-1/managers",
      ],
      [
        "/api/prohibitorum/saml-applications/{id}/managers",
        "http://localhost/api/prohibitorum/saml-applications/1/managers",
      ],
    ] as const) {
      const managers = bodyOf(read(schemaPath, config(), url)) as Array<{
        id: number;
        username: string;
        assignedAt: string;
      }>;
      expect(managers.map((manager) => manager.id)).toEqual([2, 3]);
      expect(managers[0]?.username).toBe("mock-user-2");
      expect(Number.isNaN(Date.parse(managers[0]?.assignedAt ?? ""))).toBe(
        false,
      );
    }
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
  it("signs the approved device in as one more session", () => {
    const current = writes();
    const reply = call(
      "POST",
      "/api/prohibitorum/me/devices/pair/approve",
      current,
      { code: "ABCD2345" },
    );
    expect(reply).toMatchObject({ kind: "empty", status: 204 });
    expect(applied(reply, current).lists.sessions).toBe(
      current.lists.sessions + 1,
    );

    const full = config((draft) => {
      draft.writes = true;
      draft.lists.sessions = mockListMax;
    });
    expect(
      applied(
        call("POST", "/api/prohibitorum/me/devices/pair/approve", full, {
          code: "ABCD2345",
        }),
        full,
      ).lists.sessions,
    ).toBe(mockListMax);
  });

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

describe("identity provider diagnostics", () => {
  // Both are GETs: they answer with the other reads, whether or not the
  // writes switch is on.
  it("answers the effective configuration and a run's result as reads", () => {
    const effective = read(
      "/api/prohibitorum/identity-providers/{slug}/effective-config",
      config(),
      "http://localhost/api/prohibitorum/identity-providers/provider-1/effective-config",
    );
    expect(effective?.kind).toBe("json");
    const fields =
      effective?.kind === "json"
        ? Object.keys((effective.body as { fields: object }).fields)
        : [];
    expect(fields).toEqual(
      expect.arrayContaining([
        "issuer",
        "authorizationEndpoint",
        "tokenEndpoint",
        "userinfoEndpoint",
        "jwksEndpoint",
        "tokenAuthMethod",
        "pkceMethod",
        "scopes",
      ]),
    );

    const result = read(
      "/api/prohibitorum/identity-providers/{slug}/tests/{id}",
      config(),
      "http://localhost/api/prohibitorum/identity-providers/provider-1/tests/run",
    );
    expect(result?.kind).toBe("json");
  });

  it("shapes each outcome the result dialog draws", () => {
    const result = (outcome: MockConfig["admin"]["diagnosticOutcome"]) => {
      const reply = read(
        "/api/prohibitorum/identity-providers/{slug}/tests/{id}",
        config((draft) => {
          draft.admin.diagnosticOutcome = outcome;
        }),
        "http://localhost/api/prohibitorum/identity-providers/provider-1/tests/run",
      );
      return (reply?.kind === "json" ? reply.body : {}) as DiagnosticResultView;
    };

    const succeeded = result("succeeded");
    expect(succeeded.status).toBe("succeeded");
    expect(succeeded.idToken?.json).toContain('"aud":');
    expect(succeeded.userinfo?.json).toContain('"picture":');
    expect(succeeded.identity?.subject.source).toBe("id_token");
    // The picture is filled in from UserInfo, the rest from the ID token.
    expect(succeeded.identity?.picture.source).toBe("userinfo");

    const fallback = result("fallback");
    expect(fallback.idToken).toBeUndefined();
    expect(fallback.userinfo?.json).toContain('"id":9007199254740993');
    expect(fallback.identity?.issuer.source).toBe("configuration");
    expect(fallback.stages?.find((s) => s.name === "id_token")?.status).toBe(
      "skipped",
    );

    const failed = result("failed");
    expect(failed.status).toBe("failed");
    expect(failed.identity).toBeUndefined();
    expect(failed.idToken).toBeUndefined();
    expect(failed.userinfo).toBeUndefined();
  });

  it("starts and completes a test without a request body", () => {
    expect(
      call(
        "POST",
        "/api/prohibitorum/identity-providers/{slug}/tests",
        writes(),
      )?.kind,
    ).toBe("json");
    expect(
      call(
        "POST",
        "/api/prohibitorum/identity-providers/{slug}/tests/{id}/complete",
        writes(),
      )?.kind,
    ).toBe("json");
  });
});

describe("mocked public flows", () => {
  function consentWrite(current: MockConfig, decision: string, url: string) {
    return buildMockReply(
      {
        method: "POST",
        schemaPath: "/api/prohibitorum/consent",
        url,
        body: { ticket: "t", decision },
      },
      current,
    );
  }

  it("asks about every scope the first time, one of them the app's own", () => {
    const body = bodyOf(read("/api/prohibitorum/consent")) as ConsentRequest;
    expect(body.client.displayName).toBe("Wiki");
    expect(body.scopes).toEqual([
      "openid",
      "profile",
      "email",
      "groups",
      "offline_access",
      "wiki:write",
    ]);
    expect(body.alreadyGranted).toBeUndefined();
    expect(body.account.displayName).toBe(
      defaultMockConfig.session.displayName,
    );
  });

  it("reports the first three as allowed before for more access", () => {
    const body = bodyOf(
      read(
        "/api/prohibitorum/consent",
        config((draft) => {
          draft.publicFlows.consent.grant = "incremental";
        }),
      ),
    ) as ConsentRequest;
    expect(body.alreadyGranted).toEqual(["openid", "profile", "email"]);
  });

  it("drops the logo and the policies the panel turns off", () => {
    const body = bodyOf(
      read(
        "/api/prohibitorum/consent",
        config((draft) => {
          draft.publicFlows.consent.logo = false;
          draft.publicFlows.consent.policy = false;
        }),
      ),
    ) as ConsentRequest;
    expect(body.client).toEqual({
      clientId: "wiki",
      displayName: "Wiki",
      tosUri: "https://wiki.example.test/terms",
    });
  });

  it("answers a spent ticket as the server does, for both reads", () => {
    const spent = config((draft) => {
      draft.publicFlows.consent.ticketValid = false;
    });
    for (const path of [
      "/api/prohibitorum/consent",
      "/api/prohibitorum/saml-consent",
    ]) {
      expect(read(path, spent)).toMatchObject({
        kind: "error",
        status: 400,
        code: "invalid_consent_ticket",
      });
    }
  });

  it("sends a SAML service as many attributes as the panel says, none at 0", () => {
    const attributes = (count: number) =>
      (
        bodyOf(
          read(
            "/api/prohibitorum/saml-consent",
            config((draft) => {
              draft.publicFlows.consent.samlAttributes = count;
            }),
          ),
        ) as SamlConsentRequest
      ).attributes;
    expect(attributes(3)).toEqual(["显示名称", "邮箱地址", "用户组"]);
    expect(attributes(0)).toEqual([]);
  });

  it("resumes an allowed consent at its return_to, and goes home otherwise", () => {
    const at = (returnTo: string) =>
      `http://localhost/api/prohibitorum/consent?return_to=${encodeURIComponent(returnTo)}`;
    expect(bodyOf(consentWrite(writes(), "approve", at("/apps")))).toEqual({
      redirect: "/apps",
    });
    for (const outside of ["//evil.example", "https://evil.example", ""]) {
      expect(bodyOf(consentWrite(writes(), "approve", at(outside)))).toEqual({
        redirect: "/",
      });
    }
    expect(bodyOf(consentWrite(writes(), "deny", at("/apps")))).toEqual({
      redirect: "/",
    });
  });
});

describe("mocked enrollment and federation pages", () => {
  type Preview = components["schemas"]["EnrollmentPreview"];
  const enrollment = "/api/prohibitorum/enrollments/{token}";

  function preview(overrides: (draft: MockConfig) => void = () => {}) {
    return bodyOf(read(enrollment, config(overrides))) as Preview;
  }

  it("previews what the panel sets up, with a readable preview for every intent", () => {
    for (const intent of [
      "bootstrap",
      "invite",
      "federated_register",
      "reset",
    ] as const) {
      const body = preview((draft) => {
        draft.publicFlows.enrollment.intent = intent;
      });
      expect(readEnrollmentPreview(body).intent).toBe(intent);
    }
    const reset = preview((draft) => {
      draft.publicFlows.enrollment.intent = "reset";
    });
    expect(reset.target).toEqual({
      username: "mock",
      displayName: "Mock Member",
    });
  });

  it("offers only a passkey and no providers to the first administrator", () => {
    const body = preview((draft) => {
      draft.publicFlows.enrollment.intent = "bootstrap";
      draft.publicFlows.enrollment.providers = 3;
    });
    expect(body.allowedMethods).toEqual(["passkey"]);
    expect(body.providers).toBeUndefined();
  });

  it("binds an invitation to the first provider it offers, and caps the list at three", () => {
    const body = preview((draft) => {
      draft.publicFlows.enrollment.providers = 9;
      draft.publicFlows.enrollment.bound = true;
    });
    expect(body.providers?.map((provider) => provider.displayName)).toEqual([
      "GitLab",
      "VRChat",
      "Steam",
    ]);
    expect(body.expectedUpstreamIdpSlug).toBe("gitlab");
  });

  it("answers an expired link as the server does", () => {
    expect(
      read(
        enrollment,
        config((draft) => {
          draft.publicFlows.enrollment.valid = false;
        }),
      ),
    ).toMatchObject({ kind: "error", status: 410, code: "enrollment_expired" });
  });

  it("signs the new account in with its first codes, and names the provider a bound invitation needs", () => {
    const verify = "/api/prohibitorum/enrollments/{token}/password-totp/verify";
    const current = writes();
    const reply = call("POST", verify, current, {
      token: "t",
      username: "carol",
      displayName: "Carol",
      password: "correct horse",
      secret_base32: "A".repeat(32),
      code: "123456",
    });
    const body = bodyOf(reply) as {
      session: { username: string };
      recoveryCodes: string[];
    };
    expect(body.session.username).toBe("carol");
    expect(body.recoveryCodes.every(isValidRecoveryCode)).toBe(true);
    const after = applied(reply, current);
    expect(after.session.signedIn).toBe(true);
    expect(after.session.username).toBe("carol");

    const bound = config((draft) => {
      draft.writes = true;
      draft.publicFlows.enrollment.bound = true;
    });
    expect(call("POST", verify, bound, { token: "t" })).toMatchObject({
      kind: "error",
      code: "enrollment_federation_required",
      details: { federationName: "GitLab" },
    });
  });

  it("leaves the passkey enrollment unmocked", () => {
    expect(
      call(
        "POST",
        "/api/prohibitorum/enrollments/{token}/register/begin",
        writes(),
        { token: "t" },
      ),
    ).toMatchObject({ kind: "error", code: "mock_unmocked" });
  });

  it("brings the prepared account's picture on the third read, and confirms with the offer the panel sets", () => {
    const confirm = "/api/prohibitorum/auth/federation/confirm";
    const current = writes();
    // Answering resets the count, so this starts from a first read.
    call("POST", `${confirm}/decline`, current);
    const pending = () =>
      (bodyOf(read(confirm, current)) as FederationConfirm).avatarPending;
    expect([pending(), pending(), pending()]).toEqual([true, true, false]);

    const never = config((draft) => {
      draft.publicFlows.welcome.avatarPending = "never";
    });
    expect((bodyOf(read(confirm, never)) as FederationConfirm).avatarUrl).toBe(
      undefined,
    );

    const reply = call("POST", confirm, current, {});
    expect(bodyOf(reply)).toEqual({ redirect: "/", offerLocalSignin: true });
    expect(applied(reply, current).session.signedIn).toBe(true);

    const expired = config((draft) => {
      draft.publicFlows.welcome.valid = false;
    });
    expect(read(confirm, expired)).toMatchObject({
      status: 401,
      code: "federation_state_invalid",
    });
  });

  it("walks a VRChat flow from the profile to the proof and finishes it", () => {
    const flow = "/api/prohibitorum/auth/federation/flows/{flow}";
    const current = writes();
    const first = readFederationFlow(bodyOf(read(flow, current)));
    expect(first.step).toBe("identify");

    const prepared = call("POST", `${flow}/prepare`, current, {
      flow: "f",
      identity: "usr_1",
    });
    expect(readFederationFlow(bodyOf(prepared)).step).toBe("proof");
    const proof = applied(prepared, current);
    expect(readFederationFlow(bodyOf(read(flow, proof))).proofUrl).toBe(
      "http://localhost/verify/vrchat/mock-proof",
    );

    expect(
      bodyOf(call("POST", `${flow}/verify`, proof, { flow: "f" })),
    ).toEqual({
      redirect: "/enroll/mock-federated",
    });
  });

  it("asks for the username and reports a missing proof when the panel says so", () => {
    const verify = "/api/prohibitorum/auth/federation/flows/{flow}/verify";
    const at = (change: (draft: MockConfig) => void) =>
      config((draft) => {
        draft.writes = true;
        draft.publicFlows.flow.step = "proof";
        change(draft);
      });
    expect(
      call(
        "POST",
        verify,
        at((draft) => {
          draft.publicFlows.flow.requiresLocalUsername = true;
        }),
        { flow: "f" },
      ),
    ).toMatchObject({ code: "local_username_required" });
    expect(
      call(
        "POST",
        verify,
        at((draft) => {
          draft.publicFlows.flow.proofMissing = true;
        }),
        { flow: "f" },
      ),
    ).toMatchObject({ code: "vrchat_proof_missing" });
    expect(
      bodyOf(
        call(
          "POST",
          verify,
          at((draft) => {
            draft.publicFlows.flow.intent = "link";
          }),
          { flow: "f" },
        ),
      ),
    ).toEqual({ redirect: "/security" });
  });
});
