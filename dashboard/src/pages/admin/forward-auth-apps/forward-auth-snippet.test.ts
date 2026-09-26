import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { forwardAuthSnippet } from "@/pages/admin/forward-auth-apps/forward-auth-snippet";

/**
 * The manual is the snippet's contract, not a nearby example.
 *
 * An operator setting up a protected host either reads `docs/forward-auth.md`
 * or copies what the console draws, and the two have to describe one
 * deployment. So the test reads the document and holds the generator against
 * its text. A change made to one and not the other fails here rather than in
 * someone's production Traefik.
 */
const manual = readFileSync(
  // Vitest serves this module over a non-file URL, so `import.meta.url` cannot
  // locate a file on disk; the suite runs from the dashboard directory, and the
  // manual is one level up.
  resolve(process.cwd(), "../docs/forward-auth.md"),
  "utf8",
);

const origin = "https://auth.example.com";
const host = "app.acme.io";

const snippet = () => forwardAuthSnippet({ baseUrl: origin, host });

/**
 * One fenced YAML block from the manual, from its first line to the closing
 * fence. `heading` is how the block is identified in the document.
 */
function documentedBlock(firstLine: string): string {
  const start = manual.indexOf(firstLine);
  if (start < 0) {
    throw new Error(`the manual no longer contains ${firstLine.trim()}`);
  }
  const end = manual.indexOf("```", start);
  return manual.slice(start, end).trimEnd();
}

describe("forward-auth snippet", () => {
  it("states the verify address the manual documents", () => {
    // The address is the one value the operator changes, so both sides have to
    // agree on where it comes from rather than on a hard-coded hostname.
    expect(
      documentedBlock('        address: "https://auth.example.com/'),
    ).toContain(`${origin}/api/prohibitorum/forward-auth/verify`);
    expect(snippet()).toContain(
      `address: "${origin}/api/prohibitorum/forward-auth/verify"`,
    );
  });

  it("reproduces the manual's middleware block key for key", () => {
    // The block is quoted rather than merely pattern-matched: every key of it
    // has to appear in the generated output with the same value, so a setting
    // the manual gained and the console did not is a failure here. The document
    // carries both middlewares and both routers in one fence, so the middleware
    // half is taken up to `routers:`.
    const documented = documentedBlock("    prohibitorum-forwardauth:");
    const middlewareHalf = documented.slice(
      0,
      documented.indexOf("\n  routers:"),
    );
    const generated = snippet();
    const keys = middlewareHalf
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line !== "" && !line.startsWith("#"));

    // `authResponseHeaders` and `addAuthCookiesToResponse` are the same list in
    // both; the rest are scalars that must appear verbatim.
    for (const line of keys) {
      if (line.startsWith("- ") || line === "authResponseHeaders:") continue;
      expect(generated).toContain(line);
    }
    for (const header of keys.filter((line) => line.startsWith("- Remote-"))) {
      expect(generated).toContain(header);
    }
    expect(generated).toContain("- __Host-prohibitorum_forward_auth");
  });

  it("routes the per-domain callback to Prohibitorum, not to the app", () => {
    // Without this router the OIDC callback never reaches the console, so no
    // per-domain cookie is ever planted and the host cannot be signed into. It
    // must also stay outside the forward-auth middleware: the request that
    // carries the callback has no cookie yet.
    const documented = documentedBlock(
      "    # The per-domain auth/callback path",
    );
    const generated = snippet();

    expect(documented).toContain("PathPrefix(`/.prohibitorum-forward-auth/`)");
    expect(documented).toContain("service: prohibitorum");
    expect(generated).toContain(
      `rule: "Host(\`${host}\`) && PathPrefix(\`/.prohibitorum-forward-auth/\`)"`,
    );
    expect(generated).toContain(`    ${host}-forwardauth:`);

    // The callback router carries a service of its own and no middleware list,
    // which is what keeps the guard off it.
    const callback = generated.slice(
      generated.indexOf(`    ${host}-forwardauth:`),
    );
    expect(callback).toContain("service: prohibitorum");
    expect(callback).not.toContain("prohibitorum-forwardauth");
  });

  it("strips the raw PAT before the request reaches the upstream", () => {
    // The console is a verifier, not a proxy, so an inbound `X-Prohibitorum-PAT`
    // survives to the backend unless a headers middleware removes it. The manual
    // is explicit that `authResponseHeaders` is not a substitute for that, and
    // the two middlewares are chained in the order that makes it true.
    expect(manual).toContain("strip-prohibitorum-pat");
    const block = snippet();
    expect(block).toContain("strip-prohibitorum-pat:");
    expect(block).toContain('X-Prohibitorum-PAT: ""');
    expect(block.indexOf("- prohibitorum-forwardauth")).toBeLessThan(
      block.lastIndexOf("- strip-prohibitorum-pat"),
    );
  });

  it("protects the application's own registered host", () => {
    // The per-domain cookie is host-scoped, so a rule naming any other host
    // would leave the registered one unprotected.
    expect(manual).toContain("Host(`app.acme.io`)");
    const block = snippet();
    expect(block).toContain(`rule: "Host(\`${host}\`)"`);
    expect(block).toContain(`service: ${host}-backend`);
  });

  it("does not double the slash when the origin ends in one", () => {
    const block = forwardAuthSnippet({
      baseUrl: "https://auth.example.com/",
      host,
    });
    expect(block).toContain(
      `address: "${origin}/api/prohibitorum/forward-auth/verify"`,
    );
    expect(block).not.toContain("forward-auth//verify");
  });

  it("leaves the static configuration to the operator", () => {
    // `forwardedHeaders.trustedIPs` is set on the EntryPoint in static config,
    // not in this dynamic block; the page says so in one line underneath.
    expect(manual).toContain("forwardedHeaders");
    expect(manual).toContain("trustedIPs");
    expect(snippet()).not.toContain("trustedIPs");
  });
});
