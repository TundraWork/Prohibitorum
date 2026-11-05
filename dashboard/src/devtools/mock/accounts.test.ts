import { describe, expect, it } from "vitest";
import {
  accountShorts,
  browserAccounts,
  clampAccountLimit,
  fillBrowserToLimit,
  mockAccountLimitMax,
  setBrowserAccountCount,
  setBrowserAccountRole,
  setChooserAllowed,
} from "@/devtools/mock/accounts";
import { buildMockReply, type MockReply } from "@/devtools/mock/fixtures";
import { defaultMockConfig, type MockConfig } from "@/devtools/mock/model";

function config(overrides: (draft: MockConfig) => void = () => {}): MockConfig {
  const draft = structuredClone(defaultMockConfig);
  overrides(draft);
  return draft;
}

/** Account 1 an admin, account 2 a member, account 3 an admin. */
function three(overrides: (draft: MockConfig) => void = () => {}) {
  return config((draft) => {
    draft.writes = true;
    draft.session.others = "ma";
    overrides(draft);
  });
}

/** One request; `{name}` in the path is filled from the body, as openapi-fetch does. */
function send(
  method: string,
  schemaPath: string,
  current: MockConfig,
  options: { account?: string; body?: unknown; query?: string } = {},
): MockReply | undefined {
  const path = schemaPath.replace(/\{(\w+)\}/g, (_, key: string) =>
    String((options.body as Record<string, unknown> | undefined)?.[key] ?? key),
  );
  return buildMockReply(
    {
      method,
      schemaPath,
      url: `http://localhost${path}${options.query ?? ""}`,
      body: options.body,
      account: options.account,
    },
    current,
  );
}

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

type Listed = { accountId: number; u: string; username: string };

function listed(current: MockConfig): Listed[] {
  return (
    bodyOf(send("GET", "/api/prohibitorum/auth/accounts", current)) as {
      accounts: Listed[];
    }
  ).accounts;
}

describe("mock browser account model", () => {
  it("reads the accounts from account 1 and the mask after it", () => {
    expect(browserAccounts(three()).map(({ id, role }) => [id, role])).toEqual([
      [1, "admin"],
      [2, "member"],
      [3, "admin"],
    ]);
    const gap = config((draft) => {
      draft.session.signedIn = false;
      draft.session.others = ".m?";
    });
    expect(browserAccounts(gap).map((account) => account.id)).toEqual([3]);
  });

  it("sets a count, fills to the limit and edits one account", () => {
    const draft = three();
    setBrowserAccountCount(draft, 5);
    expect(draft.session).toMatchObject({ signedIn: true, others: "mamm" });
    setBrowserAccountCount(draft, 0);
    expect(draft.session).toMatchObject({ signedIn: false, others: "" });
    draft.session.limit = 4;
    fillBrowserToLimit(draft);
    expect(browserAccounts(draft)).toHaveLength(4);
    setBrowserAccountRole(draft, 3, "admin");
    setBrowserAccountRole(draft, 1, "member");
    expect(draft.session).toMatchObject({ role: "member", others: "mam" });
    setChooserAllowed(draft, 2, false);
    expect(draft.session.chooserDenied).toBe(".x");
    setChooserAllowed(draft, 2, true);
    expect(draft.session.chooserDenied).toBe("");
  });

  it("keeps the limit within the server's range", () => {
    expect(clampAccountLimit(0)).toBe(1);
    expect(clampAccountLimit(mockAccountLimitMax + 5)).toBe(
      mockAccountLimitMax,
    );
    expect(clampAccountLimit(Number.NaN)).toBe(1);
  });

  it("derives four hex digits per account, eight where two collide", () => {
    const ids = Array.from({ length: mockAccountLimitMax }, (_, i) => i + 1);
    const shorts = accountShorts(ids);
    expect(accountShorts(ids)).toEqual(shorts);
    for (const u of shorts.values()) expect(u).toMatch(/^[0-9a-f]{4,8}$/);
    expect(new Set(shorts.values()).size).toBe(ids.length);
  });
});

describe("mocked browser accounts", () => {
  it("lists the browser's accounts in order, each keeping its reference", () => {
    const body = bodyOf(
      send("GET", "/api/prohibitorum/auth/accounts", three()),
    ) as { limit: number; accounts: Listed[] };
    expect(body.limit).toBe(10);
    expect(
      body.accounts.map(({ accountId, username }) => [accountId, username]),
    ).toEqual([
      [1, "mock"],
      [2, "mock-user-2"],
      [3, "mock-user-3"],
    ]);
    const alone = listed(
      config((draft) => {
        draft.session.signedIn = false;
        draft.session.others = ".m";
      }),
    );
    expect(alone).toHaveLength(1);
    expect(alone[0]?.u).toBe(body.accounts[2]?.u);
  });

  it("answers the signed-in reads as the account the header names", () => {
    const current = three();
    const me = "/api/prohibitorum/me";
    expect(bodyOf(send("GET", me, current, { account: "2" }))).toMatchObject({
      id: 2,
      username: "mock-user-2",
      role: "member",
    });
    expect(bodyOf(send("GET", me, current, { account: "3" }))).toMatchObject({
      id: 3,
      role: "admin",
    });
    expect(bodyOf(send("GET", me, current))).toMatchObject({
      id: 1,
      username: "mock",
    });
    expect(send("GET", me, current, { account: "9" })).toEqual({
      kind: "error",
      status: 401,
      code: "account_not_signed_in",
    });
    expect(send("GET", me, current, { account: "x" })).toMatchObject({
      status: 400,
      code: "bad_request",
    });
    expect(
      send("GET", "/api/prohibitorum/config", current, { account: "9" })?.kind,
    ).toBe("json");
    // The first account in the order is the default, even when it is not 1.
    const later = three((draft) => {
      draft.session.signedIn = false;
    });
    expect(bodyOf(send("GET", me, later))).toMatchObject({ id: 2 });
  });

  it("keeps a browser account's role in the management directory", () => {
    const current = three();
    const row = bodyOf(
      send("GET", "/api/prohibitorum/accounts/{id}", current, {
        body: { id: 3 },
      }),
    ) as { role: string };
    expect(row.role).toBe("admin");
    const reply = send("PUT", "/api/prohibitorum/accounts/{id}", current, {
      body: { id: 3, role: "user" },
    });
    expect(applied(reply, current).session.others).toBe("mm");
  });

  it("signs out the header's account alone, or every account", () => {
    const current = three();
    const logout = "/api/prohibitorum/auth/logout";
    expect(
      applied(send("POST", logout, current, { account: "2" }), current).session,
    ).toMatchObject({ signedIn: true, others: ".a" });
    expect(
      applied(send("POST", logout, current, { account: "3" }), current).session
        .others,
    ).toBe("m");
    expect(
      applied(send("POST", logout, current), current).session,
    ).toMatchObject({ signedIn: false, others: "ma" });
    expect(send("POST", logout, current, { account: "9" })).toEqual({
      kind: "empty",
      status: 204,
    });
    const all = send("POST", "/api/prohibitorum/auth/logout-all", current);
    expect(applied(all, current).session).toMatchObject({
      signedIn: false,
      others: "",
    });
  });

  it("adds a sign-in to the browser with its reference, until the limit", () => {
    const totp = "/api/prohibitorum/auth/totp/verify";
    const current = three();
    const reply = send("POST", totp, current, { body: {} });
    const body = bodyOf(reply) as {
      redirect: string;
      account: { accountId: number; u: string };
    };
    expect(body.account.accountId).toBe(4);
    expect(body.redirect).toBe(`/?u=${body.account.u}`);
    const after = applied(reply, current);
    expect(after.session.others).toBe("mam");
    expect(listed(after)[3]?.u).toBe(body.account.u);

    const full = three((draft) => {
      draft.session.limit = 3;
    });
    for (const path of [
      totp,
      "/api/prohibitorum/auth/recovery-code/verify",
      "/api/prohibitorum/auth/federation/-/confirm",
      "/api/prohibitorum/enrollments/{token}/password-totp/verify",
    ]) {
      expect(send("POST", path, full, { body: { token: "t" } })).toEqual({
        kind: "error",
        status: 409,
        code: "account_limit_reached",
      });
    }

    const alone = config((draft) => {
      draft.writes = true;
      draft.session.signedIn = false;
    });
    expect(bodyOf(send("POST", totp, alone, { body: {} }))).toMatchObject({
      redirect: "/",
      account: { accountId: 1 },
    });
  });

  it("previews an enrollment as at the limit only for an account not already here", () => {
    const enrollment = "/api/prohibitorum/enrollments/{token}";
    const full = three((draft) => {
      draft.session.limit = 3;
    });
    expect(bodyOf(send("GET", enrollment, full))).toMatchObject({
      atLimit: true,
    });
    expect(bodyOf(send("GET", enrollment, three()))).toMatchObject({
      atLimit: false,
    });
    const reset = three((draft) => {
      draft.session.limit = 3;
      draft.publicFlows.enrollment.intent = "reset";
    });
    expect(bodyOf(send("GET", enrollment, reset))).toMatchObject({
      atLimit: false,
    });
  });

  it("offers the chooser's accounts with whether each may use the app", () => {
    const chooser = "/api/prohibitorum/account-chooser";
    const current = three((draft) => {
      draft.session.chooserDenied = ".x";
    });
    const body = bodyOf(
      send("GET", chooser, current, { query: "?ticket=mock" }),
    ) as {
      app: unknown;
      accounts: (Listed & { allowed: boolean })[];
      otherAccountUrl: string;
    };
    expect(body.app).toMatchObject({
      kind: "oidc",
      key: "oidc:wiki",
      name: "Wiki",
    });
    expect(body.accounts.map((account) => account.allowed)).toEqual([
      true,
      false,
      true,
    ]);
    expect(body.accounts.map((account) => account.u)).toEqual(
      listed(current).map((account) => account.u),
    );
    expect(body.otherAccountUrl).toMatch(/^\/login\?return_to=/);
    expect(
      bodyOf(send("GET", chooser, current, { query: "?ticket=mock-saml" })),
    ).toMatchObject({ app: { kind: "saml", key: "saml:nextcloud" } });
    expect(
      send("GET", chooser, current, { query: "?ticket=expired" }),
    ).toMatchObject({ status: 404, code: "account_chooser_expired" });
  });

  it("continues as the chosen account, refusing one that may not use the app", () => {
    const chooser = "/api/prohibitorum/account-chooser";
    const current = three((draft) => {
      draft.session.chooserDenied = ".x";
    });
    const chosen = bodyOf(
      send("POST", chooser, current, {
        body: { ticket: "mock", accountId: 3 },
      }),
    ) as { redirect: string };
    expect(chosen.redirect).toBe(
      `/consent?ticket=mock&return_to=%2F&u=${listed(current)[2]?.u}`,
    );
    expect(
      send("POST", chooser, current, {
        body: { ticket: "mock", accountId: 2 },
      }),
    ).toMatchObject({ status: 403, code: "app_access_denied" });
    expect(
      send("POST", chooser, current, {
        body: { ticket: "mock", accountId: 9 },
      }),
    ).toMatchObject({ status: 401, code: "account_not_signed_in" });
    expect(
      send("POST", chooser, current, {
        body: { ticket: "expired", accountId: 1 },
      }),
    ).toMatchObject({ status: 404, code: "account_chooser_expired" });
  });

  it("sends a consent page's other-account choice to the chooser", () => {
    const current = three();
    const switchPath = "/api/prohibitorum/account-chooser/switch";
    expect(
      bodyOf(
        send("POST", switchPath, current, {
          account: "2",
          body: {
            kind: "oidc",
            consentTicket: "c",
            returnTo: "/oauth/authorize?client_id=wiki",
          },
        }),
      ),
    ).toEqual({ redirect: "/choose-account?ticket=mock" });
    expect(
      bodyOf(
        send("POST", switchPath, current, {
          body: { kind: "saml", consentTicket: "c" },
        }),
      ),
    ).toEqual({ redirect: "/choose-account?ticket=mock-saml" });
  });

  it("starts a SAML launch and an identity link for the tab's account", () => {
    const current = three();
    const launch = "/api/prohibitorum/me/apps/saml-launch";
    expect(
      bodyOf(
        send("POST", launch, current, {
          account: "2",
          query: "?sp=nextcloud",
        }),
      ),
    ).toEqual({ redirect: "/saml/sso/init?sp=nextcloud&account_choice=mock" });
    expect(send("POST", launch, current, { account: "9" })).toMatchObject({
      code: "account_not_signed_in",
    });
    expect(
      bodyOf(
        send(
          "POST",
          "/api/prohibitorum/me/identities/link/{slug}/begin",
          current,
          { body: { slug: "gitlab", returnTo: "/require-identity/t-1" } },
        ),
      ),
    ).toEqual({ redirect: "/require-identity/t-1" });
  });
});
