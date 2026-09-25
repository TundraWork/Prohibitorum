import { describe, expect, it } from "vitest";
import {
  headerNameError,
  isCidr,
  maxTrustedProxies,
  parseProxies,
} from "@/pages/admin/settings/client-ip-form";

describe("client IP header name", () => {
  it("takes the characters clientip.validHeaderName takes", () => {
    expect(headerNameError("CF-Connecting-IP")).toBeUndefined();
    expect(headerNameError("X_Real_IP")).toBeUndefined();
  });

  it("requires a name and refuses anything else", () => {
    expect(headerNameError("")?.id).toBe("settings.network.header.required");
    for (const value of ["X Real IP", " X-Real-IP", "X-Real-IP:", "Ünï"]) {
      expect(headerNameError(value)?.id).toBe(
        "settings.network.header.invalid",
      );
    }
  });
});

describe("CIDR ranges", () => {
  it("accepts IPv4 and IPv6 ranges net.ParseCIDR accepts", () => {
    for (const value of [
      "10.0.0.0/8",
      "192.168.1.7/32",
      "0.0.0.0/0",
      "10.1.2.3/8",
      "2001:db8::/32",
      "::/0",
      "::1/128",
      "fe80::1:2:3:4/64",
      "1:2:3:4:5:6:7:8/128",
      "::ffff:10.0.0.1/128",
      "1::2:3:4:5:6:7/112",
    ]) {
      expect(isCidr(value), value).toBe(true);
    }
  });

  it("refuses an address without a prefix, a prefix out of range, and malformed addresses", () => {
    for (const value of [
      "10.0.0.0",
      "10.0.0.0/33",
      "10.0.0.0/",
      "10.0.0/8",
      "10.0.0.256/8",
      "10.00.0.0/8",
      "10.0.0.0/8/8",
      "2001:db8::/129",
      "2001:db8:::/32",
      "1::2::3/64",
      "1:2:3:4:5:6:7:8:9/64",
      "1:2:3:4:5:6:7::8/64",
      "12345::/16",
      "fe80::1%eth0/64",
      "localhost/8",
      "",
    ]) {
      expect(isCidr(value), value).toBe(false);
    }
  });
});

describe("trusted proxies box", () => {
  it("reads one range per line and skips blank lines", () => {
    expect(parseProxies("10.0.0.0/8\n\n2001:db8::/32\n")).toEqual({
      ok: true,
      proxies: ["10.0.0.0/8", "2001:db8::/32"],
    });
  });

  it("reports spaces around an address rather than trimming them", () => {
    expect(parseProxies("10.0.0.0/8\n 192.168.0.0/16")).toEqual({
      ok: false,
      problem: { kind: "spaces", line: 2 },
    });
    expect(parseProxies("10.0.0.0/8\t")).toEqual({
      ok: false,
      problem: { kind: "spaces", line: 1 },
    });
  });

  it("names the first line that is not a range", () => {
    expect(parseProxies("10.0.0.0/8\n\nproxy.example")).toEqual({
      ok: false,
      problem: { kind: "invalid", line: 3 },
    });
  });

  it("wants at least one range and no more than the server keeps", () => {
    expect(parseProxies("")).toEqual({ ok: false, problem: "required" });
    expect(parseProxies("\n\n")).toEqual({ ok: false, problem: "required" });
    const lines = Array.from(
      { length: maxTrustedProxies + 1 },
      (_, index) => `10.0.${index}.0/24`,
    );
    expect(parseProxies(lines.slice(0, -1).join("\n")).ok).toBe(true);
    expect(parseProxies(lines.join("\n"))).toEqual({
      ok: false,
      problem: "too-many",
    });
  });
});
