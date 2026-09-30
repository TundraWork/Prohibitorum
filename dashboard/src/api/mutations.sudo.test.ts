import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  deleteCredentialMutationOptions,
  deleteSamlAppMutationOptions,
  revokeTokenMutationOptions,
  type UpdateAccountInput,
  updateAccountMutationOptions,
} from "@/api/mutations";
import {
  configureSudo,
  resetSudo,
  type SudoRequest,
  sudoMethodsQueryOptions,
} from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { createQueryClient } from "@/app/query-client";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const queryClient = createQueryClient(() => undefined);
let prompts: SudoRequest[];

/** Every request the mutation sent, as `METHOD path`. */
function sent(): string[] {
  return fetchBoundary.mock.calls.map(
    ([request]) => `${request.method} ${new URL(request.url).pathname}`,
  );
}

beforeEach(() => {
  prompts = [];
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  // The window is closed, so a guarded write opens the prompt before it sends
  // anything.
  queryClient.setQueryData(sudoMethodsQueryOptions().queryKey, {
    methods: ["password_totp"],
    fresh: false,
  });
  configureSudo({
    queryClient,
    set: (request) => {
      if (request) prompts.push(request);
    },
    setFresh: () => undefined,
    getFresh: () => false,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

describe("writes that ask for sudo", () => {
  it.each([
    {
      name: "removing a passkey",
      run: () =>
        deleteCredentialMutationOptions(queryClient).mutationFn?.(
          4,
          undefined as never,
        ),
      reason: sudoReason.deletePasskey,
    },
    {
      name: "revoking an access token",
      run: () =>
        revokeTokenMutationOptions(queryClient).mutationFn?.(
          4,
          undefined as never,
        ),
      reason: sudoReason.revokeToken,
    },
    {
      name: "deleting a SAML application",
      run: () =>
        deleteSamlAppMutationOptions(queryClient).mutationFn?.(
          4,
          undefined as never,
        ),
      reason: sudoReason.deleteApplication,
    },
  ])(
    "prompts before $name while the window is closed",
    async ({ run, reason }) => {
      void run();

      await vi.waitFor(() => expect(prompts).toHaveLength(1));
      expect(prompts[0]?.reason).toBe(reason);
      expect(sent()).toEqual([]);
    },
  );
});

describe("saving an account", () => {
  const body = (role: "user" | "admin"): UpdateAccountInput => ({
    displayName: "Managed Account",
    disabled: false,
    role,
  });

  it("saves a profile edit without prompting", async () => {
    fetchBoundary.mockResolvedValue(
      Response.json({ id: 7, displayName: "Managed Account", role: "user" }),
    );

    await updateAccountMutationOptions(queryClient).mutationFn?.(
      { id: 7, body: body("user"), previousRole: "user" },
      undefined as never,
    );

    expect(prompts).toEqual([]);
    expect(sent()).toEqual(["PUT /api/prohibitorum/accounts/7"]);
  });

  it("prompts before a change of role", async () => {
    void updateAccountMutationOptions(queryClient).mutationFn?.(
      { id: 7, body: body("admin"), previousRole: "user" },
      undefined as never,
    );

    await vi.waitFor(() => expect(prompts).toHaveLength(1));
    expect(prompts[0]?.reason).toBe(sudoReason.changeAccountRole);
    expect(sent()).toEqual([]);
  });
});
