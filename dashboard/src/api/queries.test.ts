import {
  MutationObserver,
  type QueryClient,
  QueryObserver,
} from "@tanstack/react-query";
import { waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { isCancellation } from "@/api/errors";
import {
  logoutMutationOptions,
  renameCredentialMutationOptions,
} from "@/api/mutations";
import {
  authStatusQueryOptions,
  clearSessionQueries,
  publicConfigQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { createQueryClient } from "@/app/query-client";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const clients: QueryClient[] = [];

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => {
  vi.useRealTimers();
  for (const client of clients) client.clear();
  clients.length = 0;
  vi.unstubAllGlobals();
});

function createClient() {
  const notices = vi.fn<(error: unknown) => void>();
  const queryClient = createQueryClient(notices);
  clients.push(queryClient);
  return { queryClient, notices };
}

describe("shared server state", () => {
  it("treats only 401 no_session as anonymous without an error notification", async () => {
    const { queryClient, notices } = createClient();
    fetchBoundary.mockResolvedValueOnce(
      Response.json(
        { code: "no_session", requestId: "anonymous" },
        { status: 401 },
      ),
    );
    expect(await queryClient.query(sessionQueryOptions())).toBeNull();
    expect(notices).not.toHaveBeenCalled();

    for (const [status, code] of [
      [401, "bad_credentials"],
      [401, "sudo_required"],
      [403, "account_disabled"],
      [503, "maintenance_mode"],
      [500, "no_session"],
    ] as const) {
      fetchBoundary.mockResolvedValueOnce(
        Response.json({ code, requestId: "session-error" }, { status }),
      );
      await expect(
        queryClient.query(sessionQueryOptions()),
      ).rejects.toMatchObject({ status, code });
    }
    expect(notices).toHaveBeenCalledTimes(5);
  });

  it("notifies once for a failed shared query and separately for independent failures", async () => {
    const { queryClient, notices } = createClient();
    fetchBoundary.mockImplementation(async () =>
      Response.json(
        { code: "server_error", requestId: "request-query" },
        { status: 500 },
      ),
    );
    const first = new QueryObserver(queryClient, authStatusQueryOptions());
    const second = new QueryObserver(queryClient, authStatusQueryOptions());
    const unsubscribeFirst = first.subscribe(() => {});
    const unsubscribeSecond = second.subscribe(() => {});
    try {
      await waitFor(() => {
        expect(first.getCurrentResult().isError).toBe(true);
        expect(second.getCurrentResult().isError).toBe(true);
      });
      expect(fetchBoundary).toHaveBeenCalledTimes(1);
      expect(notices).toHaveBeenCalledTimes(1);

      await expect(
        queryClient.query(publicConfigQueryOptions()),
      ).rejects.toMatchObject({ status: 500 });
      expect(notices).toHaveBeenCalledTimes(2);
    } finally {
      unsubscribeFirst();
      unsubscribeSecond();
    }
  });

  it("cancels and removes only protected queries before an old response can refill them", async () => {
    const { queryClient, notices } = createClient();
    let release!: (response: Response) => void;
    let requestSignal: AbortSignal | undefined;
    fetchBoundary.mockImplementation((request) => {
      requestSignal = request.signal;
      return new Promise<Response>((resolve) => {
        release = resolve;
      });
    });
    queryClient.setQueryData(["public", "config"], {
      instanceName: "Public instance",
    });
    await queryClient.query({
      queryKey: ["credential-details", 7],
      meta: { requiresSession: true },
      queryFn: async () => ({ nickname: "Old account key" }),
    });
    const pending = queryClient
      .query(sessionQueryOptions())
      .catch((error: unknown) => error);
    await waitFor(() => expect(requestSignal).toBeDefined());

    await clearSessionQueries(queryClient);
    expect(requestSignal?.aborted).toBe(true);
    expect(isCancellation(await pending)).toBe(true);
    vi.useFakeTimers();
    release(
      Response.json({
        id: 1,
        username: "old-account",
        displayName: "Old account",
        role: "user",
      }),
    );
    await vi.advanceTimersByTimeAsync(0);
    vi.useRealTimers();

    expect(queryClient.getQueryData(["session", "me"])).toBeUndefined();
    expect(queryClient.getQueryData(["credential-details", 7])).toBeUndefined();
    expect(queryClient.getQueryData(["public", "config"])).toEqual({
      instanceName: "Public instance",
    });
    expect(notices).not.toHaveBeenCalled();
  });

  it("suppresses cancelled query and mutation errors without clearing unrelated state", async () => {
    const { queryClient, notices } = createClient();
    fetchBoundary.mockRejectedValue(
      new DOMException("Cancelled", "AbortError"),
    );
    queryClient.setQueryData(["public", "config"], {
      instanceName: "Public instance",
    });
    await expect(
      queryClient.query(authStatusQueryOptions()),
    ).rejects.toMatchObject({ name: "AbortError" });
    const mutation = new MutationObserver(
      queryClient,
      logoutMutationOptions(queryClient),
    );
    await expect(mutation.mutate()).rejects.toMatchObject({
      name: "AbortError",
    });

    expect(notices).not.toHaveBeenCalled();
    expect(queryClient.getQueryData(["public", "config"])).toEqual({
      instanceName: "Public instance",
    });
  });

  it("reports a failed mutation once, without retry or treating sudo_required as logout", async () => {
    const { queryClient, notices } = createClient();
    await queryClient.query({
      ...sessionQueryOptions(),
      queryFn: async () => ({
        id: 3,
        username: "user",
        displayName: "User",
        role: "user",
      }),
    });
    fetchBoundary.mockResolvedValue(
      Response.json(
        { code: "sudo_required", requestId: "request-sudo" },
        { status: 401 },
      ),
    );
    const mutation = new MutationObserver(
      queryClient,
      renameCredentialMutationOptions(queryClient),
    );
    await expect(
      mutation.mutate({ id: 7, nickname: "Desk" }),
    ).rejects.toMatchObject({ code: "sudo_required" });

    expect(fetchBoundary).toHaveBeenCalledTimes(1);
    expect(notices).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(["session", "me"])).toMatchObject({
      id: 3,
    });
  });

  it("refreshes only credential queries after rename and clears protected cache after logout", async () => {
    const { queryClient } = createClient();
    let nickname = "Original";
    const credentials = {
      queryKey: ["session", "credentials"],
      staleTime: Infinity,
      meta: { requiresSession: true },
      queryFn: async () => [{ id: 7, nickname }],
    };
    await queryClient.query(credentials);
    const publicData = {
      queryKey: ["public", "auth-status"],
      staleTime: Infinity,
      queryFn: async () => ({ bootstrapped: true }),
    };
    await queryClient.query(publicData);
    fetchBoundary.mockImplementation(
      async () => new Response(null, { status: 204 }),
    );
    nickname = "Renamed";

    const rename = new MutationObserver(
      queryClient,
      renameCredentialMutationOptions(queryClient),
    );
    await rename.mutate({ id: 7, nickname });
    expect(await queryClient.query(credentials)).toEqual([
      { id: 7, nickname: "Renamed" },
    ]);
    expect(
      await queryClient.query({
        ...publicData,
        queryFn: async () => ({ bootstrapped: false }),
      }),
    ).toEqual({ bootstrapped: true });

    const logout = new MutationObserver(
      queryClient,
      logoutMutationOptions(queryClient),
    );
    await logout.mutate();
    expect(queryClient.getQueryData(credentials.queryKey)).toBeUndefined();
    expect(queryClient.getQueryData(publicData.queryKey)).toEqual({
      bootstrapped: true,
    });
  });
});
