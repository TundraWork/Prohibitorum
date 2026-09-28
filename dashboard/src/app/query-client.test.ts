import { msg } from "@lingui/core/macro";
import { MutationObserver } from "@tanstack/react-query";
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
