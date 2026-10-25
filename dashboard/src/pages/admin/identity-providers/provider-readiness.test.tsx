import { I18nProvider } from "@lingui/react";
import { render } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { notReadyReason } from "@/pages/admin/identity-providers/provider-readiness";
import {
  type Provider,
  providerFixture,
  publicOidcConfig,
} from "@/test/identity-provider";

beforeEach(() => {
  i18n.activate("en");
});

function said(provider: Provider, brief = false): string {
  const { container, unmount } = render(
    <I18nProvider i18n={i18n}>
      <p>{notReadyReason(provider, brief)}</p>
    </I18nProvider>,
  );
  const text = container.textContent ?? "";
  unmount();
  return text;
}

const unready = (overrides: Partial<Provider>) =>
  providerFixture({ ready: false, ...overrides });

describe("what a provider that is not ready is told", () => {
  it("never mentions a client secret to a public client", () => {
    for (const overrides of [
      { disabled: true },
      {},
      { allowSignIn: false },
      { secretConfigured: false },
    ]) {
      const text = said(
        unready({ config: publicOidcConfig, ...overrides }),
      ).toLowerCase();
      expect(text).toMatch(/valid connection|connection is not valid/);
      expect(text).not.toContain("secret");
    }
  });

  it("names the secret of a confidential client only when none is stored", () => {
    expect(said(unready({ secretConfigured: false }))).toBe(
      "Sign-in through this provider fails because no client secret is set.",
    );
    expect(said(unready({ secretConfigured: true }))).toBe(
      "Sign-in through this provider fails because the connection is not valid.",
    );
  });

  it("names the missing Steam key", () => {
    expect(said(unready({ protocol: "steam" }))).toBe(
      "Sign-in through this provider fails because no Web API key is set.",
    );
  });

  it("words a VRChat provider from the operator session it has", () => {
    const vrchat = { protocol: "vrchat", allowSignIn: false };
    expect(
      said(unready({ ...vrchat, secretStatus: "unconfigured" })),
    ).toContain("operator account is not signed in.");
    expect(said(unready({ ...vrchat, secretStatus: "invalid" }))).toContain(
      "operator session has expired.",
    );
    // A valid session is never called signed out or expired.
    const valid = said(unready({ ...vrchat, secretStatus: "valid" }));
    expect(valid).not.toMatch(/signed in|expired/);
    expect(valid).toContain("has not been checked");
  });

  it("says what stops working when sign-in is off, not that sign-in fails", () => {
    const text = said(unready({ allowSignIn: false, secretConfigured: false }));
    expect(text).toBe(
      "Linking and verification through this provider fail because no client secret is set.",
    );
    expect(text).not.toContain("Sign-in");
  });

  it("tells a disabled provider what to do before it can be enabled", () => {
    expect(said(unready({ disabled: true, secretConfigured: false }))).toBe(
      "Set a client secret before enabling this provider.",
    );
  });

  it.each([
    [{}, "Needs a valid connection"],
    [{ secretConfigured: false }, "Needs a client secret"],
    [{ protocol: "steam" }, "Needs a Web API key"],
    [
      { protocol: "vrchat", secretStatus: "unconfigured" },
      "Needs the operator account",
    ],
    [
      { protocol: "vrchat", secretStatus: "invalid" },
      "Operator session expired",
    ],
    [
      { protocol: "vrchat", secretStatus: "valid" },
      "Check the operator account",
    ],
  ])("gives the short form of the same cause for %j", (overrides, brief) => {
    expect(said(unready(overrides), true)).toBe(brief);
  });
});
