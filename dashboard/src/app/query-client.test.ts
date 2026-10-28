import { msg } from "@lingui/core/macro";
import {
  isCancelledError,
  MutationObserver,
  QueryObserver,
} from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { publicConfigQueryOptions } from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";
import { createQueryClient } from "@/app/query-client";

const saved = msg({ id: "test.saved", message: "Saved" });
const enabled = msg({ id: "test.enabled", message: "Enabled" });
const disabled = msg({ id: "test.disabled", message: "Disabled" });

it("announces a write's success message and stays quiet for writes without one", async () => {
  const notifySuccess = vi.fn();
  const queryClient = createQueryClient(() => undefined, notifySuccess);

  await new MutationObserver(queryClient, {
    mutationFn: async () => undefined,
    meta: { success: saved },
  }).mutate();
  await new MutationObserver(queryClient, {
    mutationFn: async () => undefined,
  }).mutate();

  expect(notifySuccess).toHaveBeenCalledTimes(1);
  expect(notifySuccess).toHaveBeenCalledWith(saved);
});

it("picks the message from what was sent, and says nothing when the write fails", async () => {
  const notifySuccess = vi.fn();
  const notifyError = vi.fn();
  const queryClient = createQueryClient(notifyError, notifySuccess);
  const options = {
    meta: {
      success: (variables: unknown) =>
        (variables as { disabled: boolean }).disabled ? disabled : enabled,
    },
  };

  await new MutationObserver(queryClient, {
    ...options,
    mutationFn: async (_: { disabled: boolean }) => undefined,
  }).mutate({ disabled: true });
  await expect(
    new MutationObserver(queryClient, {
      ...options,
      mutationFn: async (_: { disabled: boolean }) => {
        throw new Error("refused");
      },
    }).mutate({ disabled: false }),
  ).rejects.toThrow("refused");

  expect(notifySuccess).toHaveBeenCalledTimes(1);
  expect(notifySuccess).toHaveBeenCalledWith(disabled);
  expect(notifyError).toHaveBeenCalledTimes(1);
});

it("hands a write's error scope to the error notice, and none for a read", async () => {
  const notifyError = vi.fn();
  const queryClient = createQueryClient(notifyError);
  const refused = new Error("refused");

  await expect(
    new MutationObserver(queryClient, {
      mutationFn: async () => {
        throw refused;
      },
      meta: { errorScope: "signing-key" },
    }).mutate(),
  ).rejects.toThrow("refused");
  await expect(
    queryClient.fetchQuery({
      queryKey: ["scope-test"],
      queryFn: async () => {
        throw refused;
      },
    }),
  ).rejects.toThrow("refused");

  expect(notifyError.mock.calls).toEqual([
    [refused, "signing-key"],
    [refused, undefined],
  ]);
});

it("takes the reader to maintenance instead of reporting it, and remembers that it is on", async () => {
  const notifyError = vi.fn();
  const onMaintenance = vi.fn();
  const queryClient = createQueryClient(notifyError, undefined, onMaintenance);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
    maintenanceMode: false,
  } as PublicConfig);
  const maintenance = new ApiError({
    kind: "http",
    status: 503,
    code: "maintenance_mode",
  });
  const other = new ApiError({
    kind: "http",
    status: 500,
    code: "server_error",
  });

  await expect(
    new MutationObserver(queryClient, {
      mutationFn: async () => {
        throw maintenance;
      },
    }).mutate(),
  ).rejects.toBe(maintenance);
  await expect(
    queryClient.fetchQuery({
      queryKey: ["maintenance-test"],
      queryFn: async () => {
        throw maintenance;
      },
    }),
  ).rejects.toBe(maintenance);
  await expect(
    queryClient.fetchQuery({
      queryKey: ["other-test"],
      queryFn: async () => {
        throw other;
      },
    }),
  ).rejects.toBe(other);

  expect(onMaintenance).toHaveBeenCalledTimes(2);
  expect(
    queryClient.getQueryData(publicConfigQueryOptions().queryKey)
      ?.maintenanceMode,
  ).toBe(true);
  expect(notifyError.mock.calls).toEqual([[other, undefined]]);
});

it("leaves a write's failure to the page when the page shows it inline", async () => {
  const notifyError = vi.fn();
  const onMaintenance = vi.fn();
  const queryClient = createQueryClient(notifyError, undefined, onMaintenance);
  queryClient.setQueryData(publicConfigQueryOptions().queryKey, {
    maintenanceMode: false,
  } as PublicConfig);
  const shown = new ApiError({ kind: "http", status: 400, code: "shown" });
  const unshown = new ApiError({ kind: "http", status: 400, code: "unshown" });
  const maintenance = new ApiError({
    kind: "http",
    status: 503,
    code: "maintenance_mode",
  });
  const fail = (
    error: unknown,
    reportsInline: boolean | ((e: unknown) => boolean),
  ) =>
    expect(
      new MutationObserver(queryClient, {
        mutationFn: async () => {
          throw error;
        },
        meta: { reportsInline },
      }).mutate(),
    ).rejects.toBe(error);

  await fail(shown, true);
  await fail(shown, (error) => error === shown);
  expect(notifyError).not.toHaveBeenCalled();

  await fail(unshown, (error) => error === shown);
  expect(notifyError.mock.calls).toEqual([[unshown, undefined]]);

  await fail(maintenance, true);
  expect(onMaintenance).toHaveBeenCalledTimes(1);
  expect(notifyError).toHaveBeenCalledTimes(1);
});

/**
 * A read of `["session", "me"]` that a page observes: each fetch takes a turn
 * of the event loop and gives up when its signal aborts.
 */
function sessionRead() {
  let calls = 0;
  const queryFn = ({ signal }: { signal: AbortSignal }) => {
    calls += 1;
    const answer = `fetch ${calls}`;
    return new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => resolve(answer), 0);
      signal.addEventListener("abort", () => {
        clearTimeout(timer);
        reject(new DOMException("The request was aborted.", "AbortError"));
      });
    });
  };
  return { queryKey: ["session", "me"], queryFn, calls: () => calls };
}

it("keeps a read that joined a page's fetch when that page unmounts", async () => {
  const queryClient = createQueryClient(() => undefined);
  const read = sessionRead();
  queryClient.setQueryData(read.queryKey, "cached");
  // The page being left watches the query; mounting refetches it.
  const unsubscribe = new QueryObserver(queryClient, read).subscribe(
    () => undefined,
  );
  // Loaders of the page being opened ask for it while that fetch is on its way.
  const first = queryClient.fetchQuery(read);
  const joined = queryClient.fetchQuery(read);
  const asked = queryClient.query(read);
  unsubscribe();

  await expect(first).resolves.toBe("fetch 2");
  await expect(joined).resolves.toBe("fetch 2");
  await expect(asked).resolves.toBe("fetch 2");
  expect(read.calls()).toBe(2);
});

it("still gives up a read whose query was cancelled and removed on purpose", async () => {
  const queryClient = createQueryClient(() => undefined);
  const read = sessionRead();
  const pending = queryClient.fetchQuery(read);
  await queryClient.cancelQueries({ queryKey: read.queryKey });
  queryClient.removeQueries({ queryKey: read.queryKey });

  const error = await pending.catch((reason: unknown) => reason);
  expect(isCancelledError(error)).toBe(true);
  expect(read.calls()).toBe(1);
});

it("still gives up a read cancelled while a page watches the query", async () => {
  const queryClient = createQueryClient(() => undefined);
  const read = sessionRead();
  const unsubscribe = new QueryObserver(queryClient, read).subscribe(
    () => undefined,
  );
  const pending = queryClient.fetchQuery(read);
  await queryClient.cancelQueries({ queryKey: read.queryKey });

  const error = await pending.catch((reason: unknown) => reason);
  expect(isCancelledError(error)).toBe(true);
  expect(read.calls()).toBe(1);
  unsubscribe();
});

it("follows a read that joined a fetch an invalidation replaced", async () => {
  const queryClient = createQueryClient(() => undefined);
  const read = sessionRead();
  queryClient.setQueryData(read.queryKey, "cached");
  const unsubscribe = new QueryObserver(queryClient, read).subscribe(
    () => undefined,
  );
  const joined = queryClient.fetchQuery(read);
  await queryClient.invalidateQueries({ queryKey: read.queryKey });

  await expect(joined).resolves.toBe("fetch 2");
  expect(read.calls()).toBe(2);
  unsubscribe();
});
