import { mutationOptions, type QueryClient } from "@tanstack/react-query";
import {
  authenticateWithPasskey,
  isValidRecoveryCode,
  validateLoginResult,
} from "@/api/auth";
import { client, requireJsonData } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { paths } from "@/api/generated/schema";
import { clearSessionQueries } from "@/api/queries";
import type {
  PasswordRequest,
  RecoveryRequest,
  RecoveryResult,
  TotpRequest,
} from "@/api/raw-paths";

export type RenameCredentialInput =
  paths["/api/prohibitorum/me/credentials/rename"]["post"]["requestBody"]["content"]["application/json"];

export function logoutMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async () => {
      await client.POST("/api/prohibitorum/auth/logout");
    },
    onSuccess: () => clearSessionQueries(queryClient),
  });
}

export function renameCredentialMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    mutationFn: async (body: RenameCredentialInput) => {
      await client.POST("/api/prohibitorum/me/credentials/rename", { body });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "credentials"] }),
  });
}

export function passwordMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (body: PasswordRequest) => {
      const result = await requireJsonData(
        client.POST("/api/prohibitorum/auth/password/begin", { body }),
      );
      if (
        typeof result !== "object" ||
        result === null ||
        typeof result.partial_session_token !== "string" ||
        result.partial_session_token.length === 0
      ) {
        throw new ApiError({ kind: "invalid-response" });
      }
      return result;
    },
  });
}

export function totpMutationOptions(returnTo?: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (body: TotpRequest) => {
      const result = await requireJsonData(
        client.POST("/api/prohibitorum/auth/totp/verify", {
          body,
          ...(returnTo === undefined
            ? {}
            : { params: { query: { return_to: returnTo } } }),
        }),
      );
      return validateLoginResult(result);
    },
  });
}

export function recoveryMutationOptions(returnTo?: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (body: RecoveryRequest): Promise<RecoveryResult> => {
      const result = await requireJsonData(
        client.POST("/api/prohibitorum/auth/recovery-code/verify", {
          body,
          ...(returnTo === undefined
            ? {}
            : { params: { query: { return_to: returnTo } } }),
        }),
      );
      validateLoginResult(result);
      if (
        body.reset_authenticator &&
        (!("recovery_codes" in result) ||
          !Array.isArray(result.recovery_codes) ||
          result.recovery_codes.length === 0 ||
          !result.recovery_codes.every(
            (code) => typeof code === "string" && isValidRecoveryCode(code),
          ))
      ) {
        throw new ApiError({ kind: "invalid-response" });
      }
      return result;
    },
  });
}

export function passkeyMutationOptions(returnTo?: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: () => authenticateWithPasskey(returnTo),
  });
}
