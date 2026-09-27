import { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  completeDiagnosticMutationOptions,
  startDiagnosticMutationOptions,
} from "@/api/mutations";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => vi.unstubAllGlobals());

/**
 * The server refuses a diagnostic start or completion that carries a body at
 * all (`diagnosticEmptyBody`), so `{}` is a 400 rather than an empty request.
 * Both writes have to leave with no body.
 */
describe("diagnostic writes", () => {
  it("starts a test with an empty POST", async () => {
    fetchBoundary.mockResolvedValue(
      Response.json({
        id: "run",
        authorizationUrl: "https://idp.example.test/authorize",
        expiresAt: "2026-09-27T00:00:00Z",
      }),
    );

    const { mutationFn } = startDiagnosticMutationOptions();
    await mutationFn?.("corp-sso", undefined as never);

    const request = fetchBoundary.mock.calls[0]?.[0];
    expect(request?.method).toBe("POST");
    expect(new URL(request?.url ?? "").pathname).toBe(
      "/api/prohibitorum/identity-providers/corp-sso/tests",
    );
    expect(request?.body).toBeNull();
  });

  it("completes a test with an empty POST", async () => {
    fetchBoundary.mockResolvedValue(
      Response.json({
        status: "running",
        expiresAt: "2026-09-27T00:00:00Z",
        stages: [],
      }),
    );

    const { mutationFn } = completeDiagnosticMutationOptions(new QueryClient());
    await mutationFn?.({ slug: "corp-sso", id: "run-1" }, undefined as never);

    const request = fetchBoundary.mock.calls[0]?.[0];
    expect(request?.method).toBe("POST");
    expect(new URL(request?.url ?? "").pathname).toBe(
      "/api/prohibitorum/identity-providers/corp-sso/tests/run-1/complete",
    );
    expect(request?.body).toBeNull();
  });
});
