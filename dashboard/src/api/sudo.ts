import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import type { QueryClient } from "@tanstack/react-query";
import { atom } from "jotai";
import { client, requireJsonData } from "@/api/client";
import { ApiError, isCancellation } from "@/api/errors";
import type { SudoMethod, SudoMethods } from "@/api/raw-paths";

export const sudoQueryKey = ["session", "sudo"] as const;

export function sudoMethodsQueryOptions() {
  return {
    queryKey: [...sudoQueryKey, "methods"],
    queryFn: async ({
      signal,
    }: {
      signal: AbortSignal;
    }): Promise<SudoMethods> => {
      const result = await requireJsonData(
        client.GET("/api/prohibitorum/me/sudo/methods", { signal }),
      );
      // Narrowed to the two methods the dialog implements; an unknown one would
      // leave the user with a control that cannot finish.
      const methods = (
        Array.isArray(result.methods) ? result.methods : []
      ).filter(
        (method): method is SudoMethod =>
          method === "webauthn" || method === "password_totp",
      );
      return { methods, fresh: result.fresh === true };
    },
  };
}

/** The backend refused a write because the sudo window is not (or no longer) open. */
export function isSudoRequired(error: unknown): boolean {
  return (
    error instanceof ApiError &&
    error.status === 401 &&
    error.code === "sudo_required"
  );
}

/**
 * The user dismissed the dialog. Deliberately not an `ApiError`: a decision, not
 * a failure, so `describeError` never turns it into a message and the global
 * toast stays quiet.
 */
export class SudoCancelled extends Error {
  readonly descriptor: MessageDescriptor = msg({
    id: "sudo.cancelled",
    message: "Verification was cancelled. Nothing was changed.",
  });

  constructor() {
    super("sudo cancelled");
    this.name = "SudoCancelled";
  }
}

export function isSudoCancelled(error: unknown): error is SudoCancelled {
  return error instanceof SudoCancelled;
}

/**
 * One interception: what the user was trying to do, the wording that explains
 * why they are being asked to verify, and the callbacks that settle the promise
 * `runWithSudo` handed back.
 */
export interface SudoRequest {
  perform: () => Promise<unknown>;
  reason?: MessageDescriptor;
  resolve: (value: unknown) => void;
  reject: (error: unknown) => void;
}

/** `null` while closed. Read and written by the single mounted `SudoDialog`. */
export const sudoDialogAtom = atom<SudoRequest | null>(null);

/** Whether the last sudo read reported an open window. */
export const sudoFreshAtom = atom(false);

/**
 * Set once by the console layout. Keeping the runner out of React context is
 * what lets `runWithSudo` stay a plain function that a `mutationFn` can call
 * directly, rather than something every page has to receive through a prop.
 */
let runner: {
  queryClient: QueryClient;
  set: (request: SudoRequest | null) => void;
  setFresh: (fresh: boolean) => void;
  getFresh: () => boolean;
} | null = null;

export function configureSudo(options: {
  queryClient: QueryClient;
  set: (request: SudoRequest | null) => void;
  setFresh: (fresh: boolean) => void;
  getFresh: () => boolean;
}): void {
  runner = options;
}

export function resetSudo(): void {
  runner = null;
}

/**
 * Runs an operation the backend guards with a fresh sudo window.
 *
 * A plain async function, not a hook: mutations call it straight from
 * `mutationFn`, so no page needs a wrapper component. It reads the cached
 * `fresh` first — the backend counts a session inside `SudoTTL` of sign-in as
 * fresh, so a user who just signed in sees no dialog. When the window is not
 * open, or when the operation comes back `sudo_required` because the cache had
 * lapsed, the dialog opens and the operation is replayed exactly once after
 * verification. Backing out rejects with `SudoCancelled`. There is no retry
 * loop: the replay is not wrapped, so a second refusal reaches the caller.
 */
export async function runWithSudo<T>(
  perform: () => Promise<T>,
  reason?: MessageDescriptor,
): Promise<T> {
  if (!runner) throw new Error("The sudo dialog is not mounted.");
  const active = runner;
  if (
    active.queryClient.getQueryData(sudoMethodsQueryOptions().queryKey) ===
    undefined
  ) {
    await active.queryClient.fetchQuery(sudoMethodsQueryOptions());
  }
  const cached = active.queryClient.getQueryData<SudoMethods>(
    sudoMethodsQueryOptions().queryKey,
  );
  const fresh = active.getFresh() || cached?.fresh === true;
  active.setFresh(fresh);

  if (fresh) {
    try {
      return await perform();
    } catch (error) {
      if (!isSudoRequired(error)) throw error;
      active.setFresh(false);
    }
  }
  return (await new Promise<unknown>((resolve, reject) => {
    active.set({ perform, reason, resolve, reject });
  })) as T;
}

export { isCancellation };
