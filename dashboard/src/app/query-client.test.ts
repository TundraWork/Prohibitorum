import { msg } from "@lingui/core/macro";
import { MutationObserver } from "@tanstack/react-query";
import { expect, it, vi } from "vitest";
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
