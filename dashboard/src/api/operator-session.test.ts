import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { validateOperatorSessionMutationOptions } from "@/api/mutations";
import { configureSudo, resetSudo, sudoMethodsQueryOptions } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const queryClient = createQueryClient(() => undefined);

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient.setQueryData(sudoMethodsQueryOptions().queryKey, {
    methods: ["password_totp"],
    fresh: true,
  });
  configureSudo({
    queryClient,
    set: () => undefined,
    setFresh: () => undefined,
    getFresh: () => true,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

/**
 * The server refuses an operator-session check that carries a body at all, so
 * `{}` is a 400 rather than an empty request. The check has to leave with no
 * body.
 */
describe("operator-session check", () => {
  it("validates the session with an empty POST", async () => {
    fetchBoundary.mockResolvedValue(
      Response.json({ status: "valid", provider: { slug: "social" } }),
    );

    const { mutationFn } = validateOperatorSessionMutationOptions(queryClient);
    await mutationFn?.("social", undefined as never);

    const request = fetchBoundary.mock.calls[0]?.[0];
    expect(fetchBoundary).toHaveBeenCalledTimes(1);
    expect(request?.method).toBe("POST");
    expect(new URL(request?.url ?? "").pathname).toBe(
      "/api/prohibitorum/identity-providers/social/operator-session/validate",
    );
    expect(request?.body).toBeNull();
  });
});
