import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
} from "@simplewebauthn/browser";
import {
  startAuthentication,
  startRegistration,
} from "@simplewebauthn/browser";
import { mutationOptions, type QueryClient } from "@tanstack/react-query";
import {
  authenticateWithPasskey,
  isValidRecoveryCode,
  validateLoginResult,
} from "@/api/auth";
import { client, requireJsonData } from "@/api/client";
import { ApiError } from "@/api/errors";
import type { components, paths } from "@/api/generated/schema";
import { clearSessionQueries } from "@/api/queries";
import type {
  AppGroupView,
  ClearDecisionRequest,
  CreateGroupRequest,
  CreateInvitationRequest,
  DeleteAccountCredentialRequest,
  ManualDecisionView,
  RevokeAccountSessionRequest,
  RevokeAccountSessionsResult,
  RevokeAccountTokenRequest,
  RulePreviewPageView,
  RulePreviewRequest,
  SetAccountDisabledRequest,
  UpdateGroupRequest,
  UpsertDecisionRequest,
} from "@/api/raw-admin-paths";
import type {
  CreatedPersonalAccessToken,
  DevicePairing,
  PasswordRequest,
  RecoveryCodesResult,
  RecoveryRequest,
  RecoveryResult,
  SudoMethod,
  SudoPasswordTotpComplete,
  TotpRequest,
} from "@/api/raw-paths";
import { successMessage } from "@/api/success-messages";
import { runWithSudo, sudoMethodsQueryOptions, sudoQueryKey } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";

type AccountView = components["schemas"]["AccountView"];

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
    meta: { success: successMessage.renamePasskey },
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

export type SessionUpdate =
  paths["/api/prohibitorum/me"]["put"]["requestBody"]["content"]["application/json"];
export type ConsentRevoke =
  paths["/api/prohibitorum/me/consent/revoke"]["post"]["requestBody"]["content"]["application/json"];

export function updateProfileMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveDisplayName },
    retry: false,
    mutationFn: async (body: SessionUpdate) =>
      requireJsonData(client.PUT("/api/prohibitorum/me", { body })),
    onSuccess: (session) => {
      // One identity source: the sidebar and every page head read this cache.
      queryClient.setQueryData(["session", "me"], session);
    },
  });
}

export function revokeConsentMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removeAppAccess },
    retry: false,
    mutationFn: async (body: ConsentRevoke) => {
      await client.POST("/api/prohibitorum/me/consent/revoke", { body });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "consent"] }),
  });
}

/* ------------------------------------------------------------------ sudo -- */

/**
 * The two-step sudo exchange. `/begin` decides the shape of the `/complete`
 * body, and the server dispatches on the intent it stored at `/begin` rather
 * than on anything in the request, so the two variants must not be mixed:
 * `webauthn` submits the raw assertion, `password_totp` submits the password
 * and code.
 */
export function sudoBeginMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (method: SudoMethod) => {
      await client.POST("/api/prohibitorum/me/sudo/begin", {
        body: { method },
      });
      // password_totp answers 204: no challenge and nothing to hand the browser.
      return method;
    },
  });
}

export function sudoCompleteMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (
      body: AuthenticationResponseJSON | SudoPasswordTotpComplete,
    ) => {
      await client.POST("/api/prohibitorum/me/sudo/complete", { body });
    },
  });
}

/** Full webauthn ceremony for sudo: begin, browser prompt, complete. */
export async function completeSudoWithPasskey(): Promise<void> {
  const optionsJSON = await requireJsonData(
    client.POST("/api/prohibitorum/me/sudo/begin", {
      body: { method: "webauthn" },
    }),
  );
  const assertion = await startAuthentication({
    optionsJSON: optionsJSON as PublicKeyCredentialRequestOptionsJSON,
  });
  await client.POST("/api/prohibitorum/me/sudo/complete", { body: assertion });
}

export async function completeSudoWithPasswordTotp(
  body: SudoPasswordTotpComplete,
): Promise<void> {
  await client.POST("/api/prohibitorum/me/sudo/begin", {
    body: { method: "password_totp" },
  });
  await client.POST("/api/prohibitorum/me/sudo/complete", { body });
}

/** Reads freshness without opening the dialog; used after a successful verify. */
export async function refreshSudoState(
  queryClient: QueryClient,
): Promise<void> {
  await queryClient.invalidateQueries({ queryKey: sudoQueryKey });
  await queryClient.fetchQuery(sudoMethodsQueryOptions());
}

/* -------------------------------------------------------------- passkeys -- */

/** Full registration ceremony: begin (sudo-guarded), browser prompt, complete. */
export async function registerPasskey(nickname?: string): Promise<void> {
  const optionsJSON = await requireJsonData(
    client.POST("/api/prohibitorum/me/credentials/register/begin"),
  );
  const attestation = await startRegistration({
    optionsJSON: optionsJSON as PublicKeyCredentialCreationOptionsJSON,
  });
  await client.POST("/api/prohibitorum/me/credentials/register/complete", {
    body: attestation as RegistrationResponseJSON,
    ...(nickname === undefined || nickname === ""
      ? {}
      : { params: { query: { nickname } } }),
  });
}

export function addCredentialMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.addPasskey },
    retry: false,
    mutationFn: (nickname?: string) =>
      // The begin step is sudo-guarded, so the prompt comes first: the user
      // should know why the browser is about to ask for a passkey.
      runWithSudo(() => registerPasskey(nickname), sudoReason.addPasskey),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "credentials"] }),
  });
}

export function deleteCredentialMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removePasskey },
    retry: false,
    mutationFn: async (id: number) => {
      await client.POST("/api/prohibitorum/me/credentials/delete", {
        body: { id },
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "credentials"] }),
  });
}

/* ------------------------------------------------- password and 2FA/OTP -- */

export function setPasswordMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.changePassword },
    retry: false,
    mutationFn: async (password: string) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/me/password/set", {
            body: { password },
          }),
        sudoReason.changePassword,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "factors"] }),
  });
}

/** Replaces the authenticator alone; the password is untouched. */
export function replaceTotpMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async (body: { secret_base32: string; code: string }) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/me/totp/verify", { body }),
          ),
        sudoReason.replaceAuthenticator,
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "factors"] }),
  });
}

/** The one interface that establishes or replaces both factors together. */
export function passwordTotpMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async (body: {
      password: string;
      secret_base32: string;
      code: string;
    }): Promise<RecoveryCodesResult> =>
      requireJsonData(
        client.POST("/api/prohibitorum/me/password-totp/verify", { body }),
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "factors"] }),
  });
}

export function regenerateRecoveryCodesMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    retry: false,
    mutationFn: async (): Promise<RecoveryCodesResult> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/me/recovery-codes/regenerate"),
          ),
        sudoReason.regenerateRecoveryCodes,
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "factors"] }),
  });
}

export function revokePasswordTotpMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.revokePasswordTotp },
    retry: false,
    mutationFn: async () => {
      await runWithSudo(
        () => client.POST("/api/prohibitorum/me/auth/revoke-password-totp"),
        sudoReason.revokePasswordTotp,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "factors"] }),
  });
}

/* ------------------------------------------------------------- sessions -- */

export function revokeSessionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.endSession },
    retry: false,
    mutationFn: async (id: string) => {
      await client.POST("/api/prohibitorum/me/sessions/revoke", {
        body: { id },
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "sessions"] }),
  });
}

/* ----------------------------------------------------------- identities -- */

export function unlinkIdentityMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.unlinkIdentity },
    retry: false,
    mutationFn: async (id: number) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/me/identities/{id}/unlink", {
            params: { path: { id } },
          }),
        sudoReason.unlinkIdentity,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "identities"] }),
  });
}

/**
 * Linking leaves the SPA entirely: the backend 302s to the provider and returns
 * the browser to a console route when it is done, so this is an assignment
 * rather than a fetch the client can await.
 */
export function identityLinkUrl(
  slug: string,
  returnTo = "/security?tab=identities",
): string {
  const query = new URLSearchParams({ return_to: returnTo });
  return `/api/prohibitorum/me/identities/link/${encodeURIComponent(slug)}/begin?${query}`;
}

/* --------------------------------------------------------------- tokens -- */

export type CreateTokenInput =
  paths["/api/prohibitorum/me/tokens"]["post"]["requestBody"]["content"]["application/json"];

export function createTokenMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async (
      body: CreateTokenInput,
    ): Promise<CreatedPersonalAccessToken> =>
      requireJsonData(client.POST("/api/prohibitorum/me/tokens", { body })),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "tokens"] }),
  });
}

export function revokeTokenMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.revokeToken },
    retry: false,
    mutationFn: async (id: number) => {
      await client.POST("/api/prohibitorum/me/tokens/revoke", { body: { id } });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "tokens"] }),
  });
}

/* -------------------------------------------------------------- devices -- */

export function deviceLookupQueryOptions(code: string) {
  return {
    queryKey: ["session", "device-pairing", code] as const,
    queryFn: async ({
      signal,
    }: {
      signal: AbortSignal;
    }): Promise<DevicePairing> =>
      requireJsonData(
        client.GET("/api/prohibitorum/me/devices/pair/lookup", {
          params: { query: { code } },
          signal,
        }),
      ),
  };
}

export function approveDeviceMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.approveDevice },
    retry: false,
    mutationFn: async (code: string) => {
      // Approval is sudo-guarded, so it goes through the step-up prompt rather
      // than the bare client: the prompt replays the call once verification
      // succeeds, and reports a dismissal as a cancellation.
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/me/devices/pair/approve", {
            body: { code },
          }),
        sudoReason.approveDevice,
      );
    },
    onSuccess: (_result, code) =>
      queryClient.invalidateQueries({
        queryKey: ["session", "device-pairing", code],
      }),
  });
}

export function cancelDeviceMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.declineDevice },
    retry: false,
    mutationFn: async (code: string) => {
      await client.POST("/api/prohibitorum/me/devices/pair/cancel", {
        body: { code },
      });
    },
    onSuccess: (_result, code) =>
      queryClient.invalidateQueries({
        queryKey: ["session", "device-pairing", code],
      }),
  });
}

/* ------------------------------------------------------------ admin -- */

/**
 * Management mutations. Each guarded write wraps *its own* call in
 * `runWithSudo` rather than pushing the ceremony up to the caller, so a page
 * calls `mutate` and gets the step-up prompt as part of the mutation itself.
 *
 * Every one of them invalidates by the `["admin", ...]` prefix rather than a
 * fully-qualified key, because the account list's key carries its filters: a
 * rename has to reach every filtered variant of that list, not just the one the
 * admin happens to be looking at.
 */

/** Invalidates one account's detail plus every filtered view of the list. */
function invalidateAccount(queryClient: QueryClient, id: number) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "accounts", id] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "accounts"] }),
  ]);
}

export type UpdateAccountInput =
  paths["/api/prohibitorum/accounts/{id}"]["put"]["requestBody"]["content"]["application/json"];

/** Replaces the whole account record: an omitted `attributes` clears them. */
export function updateAccountMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveAccount },
    retry: false,
    mutationFn: async ({
      id,
      body,
    }: {
      id: number;
      body: UpdateAccountInput;
    }): Promise<AccountView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.PUT("/api/prohibitorum/accounts/{id}", {
              params: { path: { id } },
              body,
            }),
          ),
        sudoReason.updateAccount,
      ),
    onSuccess: (_account, { id }) => invalidateAccount(queryClient, id),
  });
}

/**
 * Flips only the disabled flag, independent of the profile form. The backend
 * refuses to disable an admin until they have been demoted, which surfaces as
 * an error rather than being prevented here.
 */
export function setAccountDisabledMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: {
      success: (variables) =>
        (variables as SetAccountDisabledRequest).disabled
          ? successMessage.disableAccount
          : successMessage.enableAccount,
    },
    retry: false,
    mutationFn: async (body: SetAccountDisabledRequest): Promise<AccountView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/accounts/set-disabled", { body }),
          ),
        sudoReason.setAccountDisabled,
      ),
    onSuccess: (_account, { id }) => invalidateAccount(queryClient, id),
  });
}

export function deleteAccountMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.deleteAccount },
    retry: false,
    mutationFn: async (id: number) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/accounts/delete", { body: { id } }),
        sudoReason.deleteAccount,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "accounts"] }),
  });
}

/** Reveal-once: the returned URL is shown through `SecretReveal`. */
export function reissueEnrollmentMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (id: number) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/accounts/reissue-enrollment", {
              body: { id },
            }),
          ),
        sudoReason.reissueEnrollment,
      ),
  });
}

export function deleteAccountCredentialMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.removePasskey },
    retry: false,
    mutationFn: async (body: DeleteAccountCredentialRequest) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/accounts/credentials/delete", {
            body,
          }),
        sudoReason.revokeAccountCredential,
      );
    },
    onSuccess: (_result, { accountId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "accounts", accountId, "credentials"],
      }),
  });
}

export function revokeAccountTokenMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.revokeToken },
    retry: false,
    mutationFn: async ({
      accountId,
      ...body
    }: { accountId: number } & RevokeAccountTokenRequest) => {
      await runWithSudo(
        () => client.POST("/api/prohibitorum/accounts/tokens/revoke", { body }),
        sudoReason.revokeAccountToken,
      );
    },
    onSuccess: (_result, { accountId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "accounts", accountId, "tokens"],
      }),
  });
}

/** Ending one session is reversible housekeeping, so it is not sudo-guarded. */
export function revokeAccountSessionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.endSession },
    retry: false,
    mutationFn: async ({
      accountId,
      ...body
    }: { accountId: number } & RevokeAccountSessionRequest) => {
      await client.POST("/api/prohibitorum/accounts/{id}/sessions/revoke", {
        params: { path: { id: accountId } },
        body,
      });
    },
    onSuccess: (_result, { accountId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "accounts", accountId, "sessions"],
      }),
  });
}

export function revokeAccountSessionsMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.endAllSessions },
    retry: false,
    mutationFn: async (id: number): Promise<RevokeAccountSessionsResult> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/accounts/revoke-sessions", {
              body: { id },
            }),
          ),
        sudoReason.revokeAccountSessions,
      ),
    onSuccess: (_result, id) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "accounts", id, "sessions"],
      }),
  });
}

/* ------------------------------------------------------------ groups -- */

function invalidateGroup(queryClient: QueryClient, groupId: number) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["admin", "groups"] }),
    queryClient.invalidateQueries({ queryKey: ["admin", "groups", groupId] }),
  ]);
}

export function createGroupMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.createGroup },
    retry: false,
    mutationFn: async (body: CreateGroupRequest): Promise<AppGroupView> =>
      runWithSudo(
        () =>
          requireJsonData(client.POST("/api/prohibitorum/groups", { body })),
        sudoReason.createGroup,
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "groups"] }),
  });
}

export function updateGroupMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveGroup },
    retry: false,
    mutationFn: async ({
      groupId,
      body,
    }: {
      groupId: number;
      body: UpdateGroupRequest;
    }): Promise<AppGroupView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.PUT("/api/prohibitorum/groups/{groupId}", {
              params: { path: { groupId } },
              body,
            }),
          ),
        sudoReason.updateGroup,
      ),
    onSuccess: (_group, { groupId }) => invalidateGroup(queryClient, groupId),
  });
}

/**
 * The backend refuses to delete a group an application still selects, so the
 * caller disables the control while `applicationCount` is non-zero and lets
 * `group_in_use` cover the race.
 */
export function deleteGroupMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.deleteGroup },
    retry: false,
    mutationFn: async (groupId: number) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/groups/{groupId}/delete", {
            params: { path: { groupId } },
          }),
        sudoReason.deleteGroup,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "groups"] }),
  });
}

export function upsertDecisionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async ({
      groupId,
      body,
    }: {
      groupId: number;
      body: UpsertDecisionRequest;
    }): Promise<ManualDecisionView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/groups/{groupId}/decisions", {
              params: { path: { groupId } },
              body,
            }),
          ),
        sudoReason.groupDecision,
      ),
    onSuccess: (_decision, { groupId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "groups", groupId, "decisions"],
      }),
  });
}

export function clearDecisionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async ({
      groupId,
      body,
    }: {
      groupId: number;
      body: ClearDecisionRequest;
    }) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/groups/{groupId}/decisions/clear", {
            params: { path: { groupId } },
            body,
          }),
        sudoReason.groupDecision,
      );
    },
    onSuccess: (_result, { groupId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "groups", groupId, "decisions"],
      }),
  });
}

/**
 * Validates an unsaved draft and pages the accounts it matches. A read in every
 * respect but the verb, so it is not sudo-guarded.
 */
export function rulePreviewMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (
      body: RulePreviewRequest,
    ): Promise<RulePreviewPageView> =>
      requireJsonData(
        client.POST("/api/prohibitorum/groups/rule-preview", { body }),
      ),
  });
}

/* ------------------------------------------------------- invitations -- */

export function createInvitationMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async (
      body: CreateInvitationRequest,
    ): Promise<components["schemas"]["InvitationResponse"]> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/invitations", { body }),
          ),
        sudoReason.createInvitation,
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "invitations"] }),
  });
}

/** Answers 200 with an empty body, unlike the 204s elsewhere in this group. */
export function revokeInvitationMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.revokeInvitation },
    retry: false,
    mutationFn: async (token: string) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/invitations/revoke", {
            body: { token },
          }),
        sudoReason.revokeInvitation,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "invitations"] }),
  });
}
