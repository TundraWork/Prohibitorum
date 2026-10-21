import { describe, expect, it } from "vitest";
import { loginContextTarget } from "@/components/custom/LoginShell";

const origin = "https://idp.example.test";

describe("the return_to the login-context endpoint is asked about", () => {
  it("passes this origin's authorize and SAML addresses on as written", () => {
    for (const path of [
      "/oauth/authorize?client_id=wiki",
      "/saml/sso?SAMLRequest=a%2fb&Signature=c%2Bd",
      "/saml/sso/init?sp=https%3A%2F%2Fsp.test",
    ]) {
      expect(loginContextTarget(origin + path, origin)).toBe(origin + path);
    }
  });

  it("puts the server's own path on this origin without re-encoding it", () => {
    const path = "/saml/sso?SAMLRequest=a%2fb&Signature=c%2Bd";
    expect(loginContextTarget(path, origin)).toBe(origin + path);
  });

  it("does not ask about anything else", () => {
    for (const value of [
      "https://elsewhere.example.test/oauth/authorize?client_id=wiki",
      "//elsewhere.example.test/oauth/authorize",
      "/security",
      `${origin}/oauth/token`,
      "not a url",
    ]) {
      expect(loginContextTarget(value, origin)).toBeUndefined();
    }
  });
});
