import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { ClientIpSettings } from "@/api/raw-admin-paths";

/**
 * The client-IP policy's rules, checked here in full.
 *
 * The server validates the same things (`clientip.ParseStored`) but answers any
 * failure with one `bad_request` and no field, so the form cannot learn from it
 * which line was wrong. It does trim each address and skip blank lines; this
 * form is stricter on purpose and reports an address with spaces around it
 * instead of changing what the admin typed.
 */

export type ClientIpStrategy = ClientIpSettings["strategy"];

/** `clientip.maxTrustedProxies`. */
export const maxTrustedProxies = 64;

/** `clientip.validHeaderName`: letters, digits, `-` and `_`. */
const headerNamePattern = /^[A-Za-z0-9_-]+$/;

export const clientIpMessages = {
  headerRequired: msg({
    id: "settings.network.header.required",
    message: "Enter the name of the header that carries the client address.",
  }),
  headerInvalid: msg({
    id: "settings.network.header.invalid",
    message: "Use only letters, digits, hyphens and underscores.",
  }),
  proxiesRequired: msg({
    id: "settings.network.proxies.required",
    message:
      "Enter at least one trusted proxy. Without one, this method has no effect.",
  }),
  proxiesTooMany: msg({
    id: "settings.network.proxies.too_many",
    message: "Enter at most 64 ranges.",
  }),
  /** Takes `{ line }`. */
  proxySpaces: msg({
    id: "settings.network.proxies.spaces",
    message: "Line {line}: remove the spaces around the address.",
  }),
  /** Takes `{ line }`. */
  proxyInvalid: msg({
    id: "settings.network.proxies.invalid",
    message:
      "Line {line} is not an IPv4 or IPv6 range, such as 10.0.0.0/8 or 2001:db8::/32.",
  }),
} as const;

export function headerNameError(value: string): MessageDescriptor | undefined {
  if (value === "") return clientIpMessages.headerRequired;
  if (!headerNamePattern.test(value)) return clientIpMessages.headerInvalid;
  return undefined;
}

/**
 * The problem with one line of the proxies box, numbered from 1 as the admin
 * reads it.
 */
export type ProxyLineProblem =
  | { kind: "spaces"; line: number }
  | { kind: "invalid"; line: number };

export type ProxyParse =
  | { ok: true; proxies: string[] }
  | { ok: false; problem: ProxyLineProblem | "required" | "too-many" };

/**
 * Reads the proxies box, one range per line. A blank line only separates
 * groups and is dropped; any other line is sent exactly as written.
 */
export function parseProxies(text: string): ProxyParse {
  const proxies: string[] = [];
  const lines = text.split("\n");
  for (const [index, raw] of lines.entries()) {
    // A box pasted from Windows carries `\r\n`; the `\r` is the line break,
    // not part of the address.
    const line = raw.endsWith("\r") ? raw.slice(0, -1) : raw;
    if (line.trim() === "") continue;
    if (line !== line.trim()) {
      return { ok: false, problem: { kind: "spaces", line: index + 1 } };
    }
    if (!isCidr(line)) {
      return { ok: false, problem: { kind: "invalid", line: index + 1 } };
    }
    proxies.push(line);
  }
  if (proxies.length === 0) return { ok: false, problem: "required" };
  if (proxies.length > maxTrustedProxies) {
    return { ok: false, problem: "too-many" };
  }
  return { ok: true, proxies };
}

/** The ranges as the box shows them, one per line. */
export function formatProxies(proxies: readonly string[]): string {
  return proxies.join("\n");
}

/** Whether `value` is an IPv4 or IPv6 range in CIDR notation, as Go parses it. */
export function isCidr(value: string): boolean {
  const slash = value.indexOf("/");
  if (slash === -1 || slash !== value.lastIndexOf("/")) return false;
  const address = value.slice(0, slash);
  const prefix = value.slice(slash + 1);
  if (!/^(0|[1-9]\d{0,2})$/.test(prefix)) return false;
  const bits = Number(prefix);
  if (isIPv4(address)) return bits <= 32;
  if (isIPv6(address)) return bits <= 128;
  return false;
}

/** Four decimal octets, 0–255, without leading zeros (`netip.ParseAddr`). */
export function isIPv4(value: string): boolean {
  const parts = value.split(".");
  return (
    parts.length === 4 &&
    parts.every((part) => /^(0|[1-9]\d{0,2})$/.test(part) && Number(part) < 256)
  );
}

/**
 * Eight groups of up to four hex digits, with one `::` standing for at least
 * one group of zeros and an optional dotted IPv4 tail. A zone (`%eth0`) is not
 * an address a range can hold.
 */
export function isIPv6(value: string): boolean {
  if (value.includes("%")) return false;
  let text = value;
  const lastColon = text.lastIndexOf(":");
  if (lastColon === -1) return false;
  const tail = text.slice(lastColon + 1);
  if (tail.includes(".")) {
    if (!isIPv4(tail)) return false;
    // The IPv4 tail fills the last two groups.
    text = `${text.slice(0, lastColon + 1)}0:0`;
  }
  const group = /^[0-9A-Fa-f]{1,4}$/;
  const halves = text.split("::");
  if (halves.length > 2) return false;
  if (halves.length === 1) {
    const groups = text.split(":");
    return groups.length === 8 && groups.every((part) => group.test(part));
  }
  const [head = "", rest = ""] = halves;
  const left = head === "" ? [] : head.split(":");
  const right = rest === "" ? [] : rest.split(":");
  return (
    left.length + right.length <= 7 &&
    [...left, ...right].every((part) => group.test(part))
  );
}
