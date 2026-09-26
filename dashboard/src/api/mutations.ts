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
  AppSummaryView,
  ClearDecisionRequest,
  ClientIpSettings,
  CreateForwardAuthAppRequest,
  CreateGroupRequest,
  CreateInvitationRequest,
  CreateOidcAppRequest,
  CreateOidcAppResponse,
  CreateSamlAppRequest,
  DeleteAccountCredentialRequest,
  DiagnosticResultView,
  DiagnosticStartView,
  MaintenanceSettings,
  ManagedApplicationKind,
  ManualDecisionView,
  OperatorSessionStartRequest,
  OperatorSessionVerifyRequest,
  OperatorSessionView,
  ProviderWriteBody,
  RevokeAccountSessionRequest,
  RevokeAccountSessionsResult,
  RevokeAccountTokenRequest,
  RotateOidcSecretResponse,
  RulePreviewPageView,
  RulePreviewRequest,
  SetAccountDisabledRequest,
  UpdateForwardAuthAppRequest,
  UpdateForwardAuthProjectionRequest,
  UpdateGroupRequest,
  UpdateOidcAppRequest,
  UpdateOidcProjectionRequest,
  UpdateSamlAppRequest,
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
export function identityLinkUrl(slug: string, returnTo = "/security"): string {
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

/* ------------------------------------------------------ instance settings -- */

/**
 * Every instance setting `/config` publishes — the name, the maintenance notice,
 * the icon and the sign-in background — is read from that one query, by the
 * sidebar, the header, the document title and the sign-in page alike. A write
 * therefore refreshes it and nothing else.
 */
function invalidatePublicConfig(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: ["public", "config"] });
}

/** An empty name clears the override; the instance falls back to its configuration. */
export function updateInstanceNameMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveInstanceName },
    retry: false,
    mutationFn: async (instanceName: string) => {
      await runWithSudo(
        () =>
          client.PUT("/api/prohibitorum/admin/settings", {
            body: { instanceName },
          }),
        sudoReason.updateInstanceName,
      );
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

export function updateMaintenanceMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: {
      success: (variables) =>
        (variables as MaintenanceSettings).maintenanceMode
          ? successMessage.maintenanceOn
          : successMessage.maintenanceSaved,
    },
    retry: false,
    mutationFn: async (body: MaintenanceSettings) => {
      await runWithSudo(
        () =>
          client.PUT("/api/prohibitorum/admin/settings/maintenance", { body }),
        sudoReason.updateMaintenance,
      );
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

/** The two instance images, which share their endpoints' shape and limits. */
export type InstanceImageKind = "icon" | "background";

const instanceImagePath = {
  icon: "/api/prohibitorum/admin/settings/icon",
  background: "/api/prohibitorum/admin/settings/background",
} as const;

/**
 * Uploads raw bytes, not a multipart form. The handler checks sudo itself rather
 * than through the JSON-only wrapper, and answers `sudo_required` the same way,
 * so `runWithSudo` covers it like any other guarded write.
 */
export function uploadInstanceImageMutationOptions(
  queryClient: QueryClient,
  kind: InstanceImageKind,
) {
  return mutationOptions({
    meta: {
      success:
        kind === "icon"
          ? successMessage.updateInstanceIcon
          : successMessage.updateSignInBackground,
    },
    retry: false,
    mutationFn: async (file: File) => {
      await runWithSudo(
        () =>
          client.PUT(instanceImagePath[kind], {
            body: file,
            // openapi-fetch serialises JSON by default; the endpoint wants the
            // bytes as they are.
            bodySerializer: (value) => value as BodyInit,
          }),
        kind === "icon"
          ? sudoReason.updateInstanceIcon
          : sudoReason.updateSignInBackground,
      );
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

export function removeInstanceImageMutationOptions(
  queryClient: QueryClient,
  kind: InstanceImageKind,
) {
  return mutationOptions({
    meta: {
      success:
        kind === "icon"
          ? successMessage.removeInstanceIcon
          : successMessage.removeSignInBackground,
    },
    retry: false,
    mutationFn: async () => {
      await runWithSudo(
        () => client.DELETE(instanceImagePath[kind]),
        kind === "icon"
          ? sudoReason.updateInstanceIcon
          : sudoReason.updateSignInBackground,
      );
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

/** Replaces the whole policy; the server keeps nothing from the previous one. */
export function updateClientIpMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveClientIp },
    retry: false,
    mutationFn: async (body: ClientIpSettings) => {
      await runWithSudo(
        () =>
          client.PUT("/api/prohibitorum/admin/settings/client-ip", { body }),
        sudoReason.updateClientIp,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "settings", "client-ip"],
      }),
  });
}

/* ---------------------------------------------------------- signing keys -- */

function invalidateSigningKeys(queryClient: QueryClient) {
  return queryClient.invalidateQueries({ queryKey: ["admin", "signing-keys"] });
}

/** A new key starts out pending: published for verification, never signing. */
export function generateSigningKeyMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.generateSigningKey },
    retry: false,
    mutationFn: async () =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/signing-keys/generate", {
              body: {},
            }),
          ),
        sudoReason.generateSigningKey,
      ),
    onSuccess: () => invalidateSigningKeys(queryClient),
  });
}

/**
 * The key-state writes answer `credential_not_found` for a key that is no longer
 * in the state the list showed, so they carry the signing-key error scope and
 * refresh the list whichever way they end: after a refusal the rows on screen
 * are the ones that were wrong.
 */
export function activateSigningKeyMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: {
      success: successMessage.activateSigningKey,
      errorScope: "signing-key",
    },
    retry: false,
    mutationFn: async (kid: string) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/signing-keys/{kid}/activate", {
              params: { path: { kid } },
              body: {},
            }),
          ),
        sudoReason.activateSigningKey,
      ),
    onSettled: () => invalidateSigningKeys(queryClient),
  });
}

/* ------------------------------------------------------ identity providers -- */

/** The list and the detail both live under this prefix, so one call covers both. */
function invalidateIdentityProviders(queryClient: QueryClient, slug?: string) {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: ["admin", "identity-providers"],
    }),
    ...(slug === undefined
      ? []
      : [
          queryClient.invalidateQueries({
            queryKey: ["admin", "identity-providers", slug],
          }),
        ]),
  ]);
}

export function createIdentityProviderMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.createIdentityProvider },
    retry: false,
    mutationFn: async (body: ProviderWriteBody) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/identity-providers", { body }),
          ),
        sudoReason.createIdentityProvider,
      ),
    onSuccess: () => invalidateIdentityProviders(queryClient),
  });
}

export function updateIdentityProviderMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.saveIdentityProvider },
    retry: false,
    mutationFn: async ({
      slug,
      body,
    }: {
      slug: string;
      body: ProviderWriteBody;
    }) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.PUT("/api/prohibitorum/identity-providers/{slug}", {
              params: { path: { slug } },
              body,
            }),
          ),
        sudoReason.saveIdentityProvider,
      ),
    onSuccess: (_view, { slug }) =>
      invalidateIdentityProviders(queryClient, slug),
  });
}

/** Rotating a secret never reads back; the page refetches the detail after. */
export function setIdentityProviderSecretMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.setIdentityProviderSecret },
    retry: false,
    mutationFn: async ({ slug, secret }: { slug: string; secret: string }) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/identity-providers/rotate-secret", {
            body: { slug, secret },
          }),
        sudoReason.setIdentityProviderSecret,
      );
    },
    onSuccess: (_result, { slug }) =>
      invalidateIdentityProviders(queryClient, slug),
  });
}

/**
 * No fixed `meta.success`: which line the reader gets depends on the direction
 * the flag moved, so the message is chosen from the variables.
 */
export function setIdentityProviderDisabledMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: {
      success: (variables) =>
        (variables as { disabled: boolean }).disabled
          ? successMessage.disableIdentityProvider
          : successMessage.enableIdentityProvider,
    },
    retry: false,
    mutationFn: async ({
      slug,
      disabled,
    }: {
      slug: string;
      disabled: boolean;
    }) =>
      requireJsonData(
        client.POST("/api/prohibitorum/identity-providers/set-disabled", {
          body: { slug, disabled },
        }),
      ),
    onSuccess: (view) => invalidateIdentityProviders(queryClient, view.slug),
  });
}

export function deleteIdentityProviderMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.deleteIdentityProvider },
    retry: false,
    mutationFn: async (slug: string) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/identity-providers/delete", {
            body: { slug },
          }),
        sudoReason.deleteIdentityProvider,
      );
    },
    onSuccess: () => invalidateIdentityProviders(queryClient),
  });
}

/**
 * The entity icons all share one shape: raw bytes on PUT, nothing on DELETE, and
 * sudo checked inside the handler. Either way the entity's own queries are
 * invalidated, because its iconUrl — and the cache-buster in it — just changed.
 */
export type EntityIconTarget =
  | { kind: "identity-provider"; slug: string }
  | { kind: ManagedApplicationKind; appId: string };

/** One prefix per application family, covering its list and its detail. */
const applicationQueryPrefix = {
  oidc: ["admin", "oidc-applications"],
  saml: ["admin", "saml-applications"],
  forward_auth: ["admin", "forward-auth-apps"],
} as const;

/**
 * A write to an application touches more than its own record: `disabled` and the
 * access restriction both show in the list, so the whole family is invalidated
 * rather than just the detail. Anything that reads the access workspace or the
 * manager list is invalidated by its own mutation.
 */
function invalidateApplication(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
  appId: string,
) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: applicationQueryPrefix[kind] }),
    queryClient.invalidateQueries({
      queryKey: ["admin", "managed-applications", kind, appId],
    }),
  ]);
}

function invalidateEntityIcon(
  queryClient: QueryClient,
  target: EntityIconTarget,
) {
  return target.kind === "identity-provider"
    ? invalidateIdentityProviders(queryClient, target.slug)
    : invalidateApplication(queryClient, target.kind, target.appId);
}

export function uploadEntityIconMutationOptions(
  queryClient: QueryClient,
  target: EntityIconTarget,
) {
  return mutationOptions({
    meta: { success: successMessage.updateEntityIcon },
    retry: false,
    mutationFn: async (file: File) => {
      // openapi-fetch serializes JSON by default; these endpoints want the bytes.
      const options = {
        body: file,
        bodySerializer: (value: Blob) => value as BodyInit,
      };
      await runWithSudo(() => {
        if (target.kind === "identity-provider") {
          return client.PUT(
            "/api/prohibitorum/identity-providers/{slug}/icon",
            {
              params: { path: { slug: target.slug } },
              ...options,
            },
          );
        }
        if (target.kind === "saml") {
          return client.PUT("/api/prohibitorum/saml-applications/{id}/icon", {
            params: { path: { id: Number(target.appId) } },
            ...options,
          });
        }
        return target.kind === "oidc"
          ? client.PUT("/api/prohibitorum/oidc-applications/{clientId}/icon", {
              params: { path: { clientId: target.appId } },
              ...options,
            })
          : client.PUT("/api/prohibitorum/forward-auth-apps/{clientId}/icon", {
              params: { path: { clientId: target.appId } },
              ...options,
            });
      }, sudoReason.updateEntityIcon);
    },
    onSuccess: () => invalidateEntityIcon(queryClient, target),
  });
}

export function removeEntityIconMutationOptions(
  queryClient: QueryClient,
  target: EntityIconTarget,
) {
  return mutationOptions({
    meta: { success: successMessage.removeEntityIcon },
    retry: false,
    mutationFn: async () => {
      await runWithSudo(() => {
        if (target.kind === "identity-provider") {
          return client.DELETE(
            "/api/prohibitorum/identity-providers/{slug}/icon",
            { params: { path: { slug: target.slug } } },
          );
        }
        if (target.kind === "saml") {
          return client.DELETE(
            "/api/prohibitorum/saml-applications/{id}/icon",
            {
              params: { path: { id: Number(target.appId) } },
            },
          );
        }
        return target.kind === "oidc"
          ? client.DELETE(
              "/api/prohibitorum/oidc-applications/{clientId}/icon",
              {
                params: { path: { clientId: target.appId } },
              },
            )
          : client.DELETE(
              "/api/prohibitorum/forward-auth-apps/{clientId}/icon",
              { params: { path: { clientId: target.appId } } },
            );
      }, sudoReason.removeEntityIcon);
    },
    onSuccess: () => invalidateEntityIcon(queryClient, target),
  });
}

/* ----------------------------------------------------- OIDC diagnostics -- */

/**
 * Effective configuration. Not a `useQuery` factory: the endpoint discovers
 * against the upstream and is rate limited, so the page fetches it on demand.
 */
export function refreshEffectiveConfigMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (slug: string) =>
      requireJsonData(
        client.GET(
          "/api/prohibitorum/identity-providers/{slug}/effective-config",
          {
            params: { path: { slug } },
          },
        ),
      ),
  });
}

/**
 * Starts a test run. The page navigates the browser to `authorizationUrl`, so
 * this one returns its result rather than invalidating anything.
 */
export function startDiagnosticMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (slug: string): Promise<DiagnosticStartView> =>
      requireJsonData(
        client.POST("/api/prohibitorum/identity-providers/{slug}/tests", {
          params: { path: { slug } },
          body: {},
        }),
      ),
  });
}

/** Marks the callback as received; the read that follows carries the result. */
export function completeDiagnosticMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async ({
      slug,
      id,
    }: {
      slug: string;
      id: string;
    }): Promise<DiagnosticResultView> =>
      requireJsonData(
        client.POST(
          "/api/prohibitorum/identity-providers/{slug}/tests/{id}/complete",
          { params: { path: { slug, id } }, body: {} },
        ),
      ),
    onSuccess: (_result, { slug, id }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "identity-providers", slug, "tests", id],
      }),
  });
}

/* -------------------------------------------------- VRChat operator session -- */

function invalidateOperatorSession(queryClient: QueryClient, slug: string) {
  return invalidateIdentityProviders(queryClient, slug);
}

export function startOperatorSessionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async ({
      slug,
      body,
    }: {
      slug: string;
      body: OperatorSessionStartRequest;
    }): Promise<OperatorSessionView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST(
              "/api/prohibitorum/identity-providers/{slug}/operator-session/start",
              { params: { path: { slug } }, body },
            ),
          ),
        sudoReason.operatorSession,
      ),
    onSuccess: (_result, { slug }) =>
      invalidateOperatorSession(queryClient, slug),
  });
}

export function verifyOperatorSessionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async ({
      slug,
      body,
    }: {
      slug: string;
      body: OperatorSessionVerifyRequest;
    }): Promise<OperatorSessionView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST(
              "/api/prohibitorum/identity-providers/{slug}/operator-session/verify",
              { params: { path: { slug } }, body },
            ),
          ),
        sudoReason.operatorSession,
      ),
    onSuccess: (_result, { slug }) =>
      invalidateOperatorSession(queryClient, slug),
  });
}

export function validateOperatorSessionMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.validateOperatorSession },
    retry: false,
    mutationFn: async (slug: string): Promise<OperatorSessionView> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST(
              "/api/prohibitorum/identity-providers/{slug}/operator-session/validate",
              { params: { path: { slug } }, body: {} },
            ),
          ),
        sudoReason.operatorSession,
      ),
    onSuccess: (_result, slug) => invalidateOperatorSession(queryClient, slug),
  });
}

export function retireSigningKeyMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: {
      success: successMessage.retireSigningKey,
      errorScope: "signing-key",
    },
    retry: false,
    mutationFn: async (kid: string) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/signing-keys/{kid}/retire", {
              params: { path: { kid } },
              body: {},
            }),
          ),
        sudoReason.retireSigningKey,
      ),
    onSettled: () => invalidateSigningKeys(queryClient),
  });
}

/* ------------------------------------------------------ OIDC applications -- */

/**
 * The create answers with a client secret once, for a confidential client. The
 * page reveals it and only then navigates; nothing here caches it.
 */
export function createOidcAppMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (
      body: CreateOidcAppRequest,
    ): Promise<CreateOidcAppResponse> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/oidc-applications", { body }),
          ),
        sudoReason.createApplication,
      ),
  });
}

export function updateOidcAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveOidcApp },
    retry: false,
    mutationFn: async ({
      clientId,
      body,
    }: {
      clientId: string;
      body: UpdateOidcAppRequest;
    }) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.PUT("/api/prohibitorum/oidc-applications/{clientId}", {
              params: { path: { clientId } },
              body,
            }),
          ),
        sudoReason.saveApplication,
      ),
    onSuccess: (_view, { clientId }) =>
      invalidateApplication(queryClient, "oidc", clientId),
  });
}

/** The projection write is not sudo-gated: it changes no credential. */
export function updateOidcProjectionMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveIdentityProjection },
    retry: false,
    mutationFn: async ({
      clientId,
      body,
    }: {
      clientId: string;
      body: UpdateOidcProjectionRequest;
    }) =>
      requireJsonData(
        client.PUT(
          "/api/prohibitorum/oidc-applications/{clientId}/identity-projection",
          { params: { path: { clientId } }, body },
        ),
      ),
    onSuccess: (_view, { clientId }) =>
      invalidateApplication(queryClient, "oidc", clientId),
  });
}

/** Rotating answers with the new secret, which the page reveals once. */
export function rotateOidcSecretMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    mutationFn: async (clientId: string): Promise<RotateOidcSecretResponse> =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/oidc-applications/rotate-secret", {
              body: { clientId },
            }),
          ),
        sudoReason.rotateClientSecret,
      ),
    onSuccess: (_result, clientId) =>
      invalidateApplication(queryClient, "oidc", clientId),
  });
}

export function deleteOidcAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.deleteApp },
    retry: false,
    mutationFn: async (clientId: string) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/oidc-applications/delete", {
            body: { clientId },
          }),
        sudoReason.deleteApplication,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: applicationQueryPrefix.oidc }),
  });
}

/* ------------------------------------------------------------ forward auth -- */

export function createForwardAuthAppMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (body: CreateForwardAuthAppRequest) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/forward-auth-apps", { body }),
          ),
        sudoReason.createApplication,
      ),
  });
}

export function updateForwardAuthAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveOidcApp },
    retry: false,
    mutationFn: async ({
      clientId,
      body,
    }: {
      clientId: string;
      body: UpdateForwardAuthAppRequest;
    }) =>
      runWithSudo(
        () =>
          requireJsonData(
            client.PUT("/api/prohibitorum/forward-auth-apps/{clientId}", {
              params: { path: { clientId } },
              body,
            }),
          ),
        sudoReason.saveApplication,
      ),
    onSuccess: (_view, { clientId }) =>
      invalidateApplication(queryClient, "forward_auth", clientId),
  });
}

export function updateForwardAuthProjectionMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.saveIdentityProjection },
    retry: false,
    mutationFn: async ({
      clientId,
      body,
    }: {
      clientId: string;
      body: UpdateForwardAuthProjectionRequest;
    }) =>
      requireJsonData(
        client.PUT(
          "/api/prohibitorum/forward-auth-apps/{clientId}/identity-projection",
          { params: { path: { clientId } }, body },
        ),
      ),
    onSuccess: (_view, { clientId }) =>
      invalidateApplication(queryClient, "forward_auth", clientId),
  });
}

export function deleteForwardAuthAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.deleteApp },
    retry: false,
    mutationFn: async (clientId: string) => {
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/forward-auth-apps/delete", {
            body: { clientId },
          }),
        sudoReason.deleteApplication,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({
        queryKey: applicationQueryPrefix.forward_auth,
      }),
  });
}

/* ------------------------------------------------------ SAML applications -- */

/**
 * SAML creation needs no step-up: it establishes no credential and grants no
 * access, so it goes straight through.
 */
export function createSamlAppMutationOptions() {
  return mutationOptions({
    retry: false,
    mutationFn: async (body: CreateSamlAppRequest) =>
      requireJsonData(
        client.POST("/api/prohibitorum/saml-applications", { body }),
      ),
  });
}

export function updateSamlAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveOidcApp },
    retry: false,
    mutationFn: async ({
      id,
      body,
    }: {
      id: number;
      body: UpdateSamlAppRequest;
    }) =>
      requireJsonData(
        client.PUT("/api/prohibitorum/saml-applications/{id}", {
          params: { path: { id } },
          body,
        }),
      ),
    onSuccess: (_view, { id }) =>
      invalidateApplication(queryClient, "saml", String(id)),
  });
}

/**
 * Re-imports metadata, which replaces the ACS endpoints and signing
 * certificates while keeping the Entity ID and name. No sudo: the record's
 * identity does not change.
 */
export function reingestSamlMetadataMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.reingestMetadata },
    retry: false,
    mutationFn: async ({
      id,
      metadataXml,
    }: {
      id: number;
      metadataXml: string;
    }) =>
      requireJsonData(
        client.POST(
          "/api/prohibitorum/saml-applications/{id}/reingest-metadata",
          {
            params: { path: { id } },
            body: { metadataXml },
          },
        ),
      ),
    onSuccess: (_view, { id }) =>
      invalidateApplication(queryClient, "saml", String(id)),
  });
}

export function deleteSamlAppMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.deleteApp },
    retry: false,
    mutationFn: async (id: number) => {
      await client.POST("/api/prohibitorum/saml-applications/delete", {
        body: { id },
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: applicationQueryPrefix.saml }),
  });
}

/* --------------------------------------------------- enable / disable any -- */

/**
 * One factory for all three families: the flag lives on every application and
 * the section that flips it is the same wherever it appears.
 */
export function setAppDisabledMutationOptions(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
) {
  return mutationOptions({
    meta: {
      success: (variables) =>
        (variables as { disabled: boolean }).disabled
          ? successMessage.disableApp
          : successMessage.enableApp,
    },
    retry: false,
    mutationFn: async ({
      appId,
      disabled,
    }: {
      appId: string;
      disabled: boolean;
    }) => {
      if (kind === "oidc") {
        return requireJsonData(
          client.POST("/api/prohibitorum/oidc-applications/set-disabled", {
            body: { clientId: appId, disabled },
          }),
        );
      }
      if (kind === "forward_auth") {
        return requireJsonData(
          client.POST("/api/prohibitorum/forward-auth-apps/set-disabled", {
            body: { clientId: appId, disabled },
          }),
        );
      }
      return requireJsonData(
        client.POST("/api/prohibitorum/saml-applications/set-disabled", {
          body: { id: Number(appId), disabled },
        }),
      );
    },
    onSuccess: (_view, { appId }) =>
      invalidateApplication(queryClient, kind, appId),
  });
}

/* --------------------------------------------------------------- access -- */

export function setAppAccessRestrictedMutationOptions(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
) {
  return mutationOptions({
    meta: {
      success: (variables) =>
        (variables as { restricted: boolean }).restricted
          ? successMessage.restrictAppAccess
          : successMessage.openAppAccess,
    },
    retry: false,
    mutationFn: async ({
      appId,
      restricted,
    }: {
      appId: string;
      restricted: boolean;
    }): Promise<AppSummaryView> =>
      requireJsonData(
        client.POST(
          "/api/prohibitorum/managed-applications/{kind}/{appId}/access/set-restricted",
          { params: { path: { kind, appId } }, body: { restricted } },
        ),
      ),
    onSuccess: (_view, { appId }) =>
      invalidateApplication(queryClient, kind, appId),
  });
}

/**
 * Replaces the whole selected-group list, so adding and removing are one call
 * carrying the ids that should end up selected.
 */
export function replaceAppGroupsMutationOptions(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
) {
  return mutationOptions({
    meta: { success: successMessage.saveAppGroups },
    retry: false,
    mutationFn: async ({
      appId,
      groupIds,
    }: {
      appId: string;
      groupIds: number[];
    }): Promise<AppGroupView[]> =>
      requireJsonData(
        client.PUT(
          "/api/prohibitorum/managed-applications/{kind}/{appId}/groups",
          {
            params: { path: { kind, appId } },
            body: { groupIds },
          },
        ),
      ),
    onSuccess: (_groups, { appId }) =>
      invalidateApplication(queryClient, kind, appId),
  });
}

/* ------------------------------------------------------------- managers -- */

/**
 * Assigning and removing are admin-only and sudo-gated. The manager list is not
 * paged and is only ever read by an admin, so invalidating it is enough — the
 * application's own queries do not carry it.
 */
export function assignAppManagerMutationOptions(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
) {
  return mutationOptions({
    meta: { success: successMessage.assignAppManager },
    retry: false,
    mutationFn: async ({
      appId,
      accountId,
    }: {
      appId: string;
      accountId: number;
    }) => {
      await runWithSudo(
        () =>
          kind === "saml"
            ? client.POST("/api/prohibitorum/saml-applications/{id}/managers", {
                params: { path: { id: Number(appId) } },
                body: { accountId },
              })
            : kind === "oidc"
              ? client.POST(
                  "/api/prohibitorum/oidc-applications/{clientId}/managers",
                  {
                    params: { path: { clientId: appId } },
                    body: { accountId },
                  },
                )
              : client.POST(
                  "/api/prohibitorum/forward-auth-apps/{clientId}/managers",
                  {
                    params: { path: { clientId: appId } },
                    body: { accountId },
                  },
                ),
        sudoReason.assignAppManager,
      );
    },
    onSuccess: (_result, { appId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "managed-applications", kind, appId, "managers"],
      }),
  });
}

export function removeAppManagerMutationOptions(
  queryClient: QueryClient,
  kind: ManagedApplicationKind,
) {
  return mutationOptions({
    meta: { success: successMessage.removeAppManager },
    retry: false,
    mutationFn: async ({
      appId,
      accountId,
    }: {
      appId: string;
      accountId: number;
    }) => {
      await runWithSudo(
        () =>
          kind === "saml"
            ? client.POST(
                "/api/prohibitorum/saml-applications/{id}/managers/remove",
                {
                  params: { path: { id: Number(appId) } },
                  body: { accountId },
                },
              )
            : kind === "oidc"
              ? client.POST(
                  "/api/prohibitorum/oidc-applications/{clientId}/managers/remove",
                  {
                    params: { path: { clientId: appId } },
                    body: { accountId },
                  },
                )
              : client.POST(
                  "/api/prohibitorum/forward-auth-apps/{clientId}/managers/remove",
                  {
                    params: { path: { clientId: appId } },
                    body: { accountId },
                  },
                ),
        sudoReason.removeAppManager,
      );
    },
    onSuccess: (_result, { appId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "managed-applications", kind, appId, "managers"],
      }),
  });
}
