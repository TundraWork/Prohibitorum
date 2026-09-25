import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError, isCancellation } from "@/api/errors";
import {
  configureSudo,
  isSudoCancelled,
  resetSudo,
  runWithSudo,
  SudoCancelled,
  type SudoRequest,
  sudoMethodsQueryOptions,
  sudoQueryKey,
} from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();

beforeEach(() => {
  fetchBoundary.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
});

afterEach(() => {
  resetSudo();
  vi.unstubAllGlobals();
});

/** Answers the freshness probe with `fresh`, then lets the operation run. */
function sudoServer(fresh: boolean) {
  fetchBoundary.mockImplementation(async (request) => {
    if (new URL(request.url).pathname.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh });
    }
    return new Response(null, { status: 204 });
  });
}

function harness(fresh: boolean) {
  const queryClient = createQueryClient(() => undefined);
  let request: SudoRequest | null = null;
  let reported = fresh;
  configureSudo({
    queryClient,
    set: (next) => {
      request = next;
    },
    setFresh: (value) => {
      reported = value;
    },
    getFresh: () => reported,
  });
  return {
    queryClient,
    get request() {
      return request;
    },
    get reported() {
      return reported;
    },
  };
}

const sudoRequired = () =>
  new ApiError({ kind: "http", status: 401, code: "sudo_required" });

describe("sudo runner", () => {
  it("runs the operation directly when the cached window is still fresh", async () => {
    sudoServer(true);
    const ui = harness(true);
    const perform = vi.fn(async () => "done");

    expect(await runWithSudo(perform)).toBe("done");
    expect(perform).toHaveBeenCalledTimes(1);
    // No prompt: nothing was parked for the dialog.
    expect(ui.request).toBeNull();
  });

  it("asks only after verification and replays the operation exactly once", async () => {
    sudoServer(false);
    const ui = harness(false);
    const perform = vi.fn(async () => "replayed");

    const pending = runWithSudo(perform, undefined);
    // The operation must not run before the user has verified anything.
    await vi.waitFor(() => expect(ui.request).not.toBeNull());
    expect(perform).not.toHaveBeenCalled();

    // What the dialog does on success: replay the parked operation, then
    // hand the result back to the waiting caller.
    const parked = ui.request as SudoRequest;
    parked.resolve(await parked.perform());

    expect(await pending).toBe("replayed");
    expect(perform).toHaveBeenCalledTimes(1);
  });

  it("prompts when a 'fresh' cache turns out to be stale, not a second time after", async () => {
    sudoServer(true);
    const ui = harness(true);
    let rejectFirst = true;
    const perform = vi.fn(async () => {
      if (rejectFirst) {
        rejectFirst = false;
        throw sudoRequired();
      }
      return "second";
    });

    const pending = runWithSudo(perform);
    await vi.waitFor(() => expect(ui.request).not.toBeNull());
    // One refused attempt, then the prompt.
    expect(perform).toHaveBeenCalledTimes(1);
    expect(ui.reported).toBe(false);

    const parked = ui.request as SudoRequest;
    parked.resolve(await parked.perform());

    expect(await pending).toBe("second");
    // Exactly one replay: no retry loop.
    expect(perform).toHaveBeenCalledTimes(2);
  });

  it("settles as cancelled without running the operation when the user backs out", async () => {
    sudoServer(false);
    const ui = harness(false);
    const perform = vi.fn(async () => "never");

    const pending = runWithSudo(perform);
    await vi.waitFor(() => expect(ui.request).not.toBeNull());

    const parked = ui.request as SudoRequest;
    parked.reject(new SudoCancelled());

    const error = await pending.catch((failure: unknown) => failure);
    expect(isSudoCancelled(error)).toBe(true);
    // Backing out is a decision, so pages and the error toast stay quiet.
    expect(isCancellation(error)).toBe(true);
    expect(perform).not.toHaveBeenCalled();
  });

  it("propagates a failure that is not about sudo", async () => {
    sudoServer(true);
    harness(true);
    const failure = new ApiError({
      kind: "http",
      status: 400,
      code: "bad_request",
    });

    await expect(
      runWithSudo(async () => {
        throw failure;
      }),
    ).rejects.toBe(failure);
  });

  it("reads freshness from the server once before deciding", async () => {
    sudoServer(true);
    const ui = harness(true);
    await runWithSudo(async () => "ok");

    const paths = fetchBoundary.mock.calls.map(
      ([request]) => new URL(request.url).pathname,
    );
    expect(paths).toEqual(["/api/prohibitorum/me/sudo/methods"]);
    expect(ui.queryClient.getQueryData(sudoQueryKey)).toBeUndefined();
    expect(
      ui.queryClient.getQueryData(sudoMethodsQueryOptions().queryKey),
    ).toMatchObject({ methods: ["password_totp"] });
  });

  it("ignores a method it cannot drive instead of offering a dead control", async () => {
    fetchBoundary.mockImplementation(async () =>
      Response.json({ methods: ["webauthn", "recovery_code"], fresh: true }),
    );
    const ui = harness(true);
    await runWithSudo(async () => "ok");

    expect(
      ui.queryClient.getQueryData(sudoMethodsQueryOptions().queryKey),
    ).toMatchObject({ methods: ["webauthn"] });
  });
});
