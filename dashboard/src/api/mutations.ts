import type { MessageDescriptor } from "@lingui/core";
import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";
import { startAuthentication } from "@simplewebauthn/browser";
import { mutationOptions, type QueryClient } from "@tanstack/react-query";
import {
  authenticateWithPasskey,
  completePasskeyLogin,
  isValidRecoveryCode,
  type PasskeyAssertion,
  registerWithPasskey,
  validateLoginResult,
} from "@/api/auth";
import { client, requireJsonData } from "@/api/client";
import { ApiError } from "@/api/errors";
import { readFederationFlow } from "@/api/federation";
import type { components, paths } from "@/api/generated/schema";
import { clearSessionQueries, myAvatarQueryOptions } from "@/api/queries";
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
  LoginAppearanceWrite,
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
  EnrollmentAccountFields,
  EnrollmentPasswordTotpResult,
  FederationConfirmResult,
  PairingStart,
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
type MyAvatarView = components["schemas"]["MyAvatarView"];

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

/**
 * An OIDC consent decision. It carries no success message: the page leaves as
 * soon as it answers, for the authorization or the application's callback.
 */
export function consentDecisionMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: ({
      ticket,
      returnTo,
      decision,
    }: {
      ticket: string;
      returnTo: string;
      decision: "approve" | "deny";
    }) =>
      requireJsonData(
        client.POST("/api/prohibitorum/consent", {
          params: { query: { return_to: returnTo } },
          body: { ticket, decision },
        }),
      ).then(validateLoginResult),
  });
}

/** A SAML consent decision; see `consentDecisionMutationOptions`. */
export function samlConsentDecisionMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: (body: { ticket: string; decision: "approve" | "decline" }) =>
      requireJsonData(
        client.POST("/api/prohibitorum/saml-consent", { body }),
      ).then(validateLoginResult),
  });
}

/* ------------------------------------------------ enrollment and federation -- */

/**
 * Creates the account an enrollment link describes with a passkey. `fields`
 * is what the reader chose; a reset sends none. The answer signs the new
 * account in, so it carries no success message: the page leaves at once.
 */
export function enrollPasskeyMutationOptions(token: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: (fields: EnrollmentAccountFields | undefined) =>
      registerWithPasskey(
        () =>
          requireJsonData(
            client.POST(
              "/api/prohibitorum/enrollments/{token}/register/begin",
              {
                params: { path: { token } },
                ...(fields ? { body: fields } : {}),
              },
            ),
          ),
        (attestation) =>
          requireJsonData(
            client.POST(
              "/api/prohibitorum/enrollments/{token}/register/complete",
              { params: { path: { token } }, body: attestation },
            ),
          ),
      ),
  });
}

/** Creates the enrollment's account with a password and an authenticator. */
export function enrollPasswordTotpMutationOptions(token: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: (
      body: EnrollmentAccountFields & {
        password: string;
        secret_base32: string;
        code: string;
      },
    ): Promise<EnrollmentPasswordTotpResult> =>
      requireJsonData(
        client.POST(
          "/api/prohibitorum/enrollments/{token}/password-totp/verify",
          { params: { path: { token } }, body },
        ),
      ),
  });
}

/** Accepts the account a first sign-in through a provider prepared. */
export function federationConfirmMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: async (): Promise<FederationConfirmResult> => {
      const result = await requireJsonData(
        client.POST("/api/prohibitorum/auth/federation/confirm", { body: {} }),
      );
      if (
        typeof result.redirect !== "string" ||
        typeof result.offerLocalSignin !== "boolean"
      ) {
        throw new ApiError({ kind: "invalid-response" });
      }
      return result;
    },
  });
}

/** Turns down the prepared account; the sign-in ends there. */
export function federationDeclineMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: async () => {
      await client.POST("/api/prohibitorum/auth/federation/confirm/decline");
    },
  });
}

/** Names the VRChat profile to verify; the flow moves on to its proof step. */
export function federationFlowPrepareMutationOptions(flow: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: async (identity: string) =>
      readFederationFlow(
        await requireJsonData(
          client.POST(
            "/api/prohibitorum/auth/federation/flows/{flow}/prepare",
            {
              params: { path: { flow } },
              body: { identity },
            },
          ),
        ),
      ),
  });
}

/** Checks the profile's bio for the proof link and finishes the flow. */
export function federationFlowVerifyMutationOptions(flow: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "federation" },
    mutationFn: (localUsername: string | undefined) =>
      requireJsonData(
        client.POST("/api/prohibitorum/auth/federation/flows/{flow}/verify", {
          params: { path: { flow } },
          body: localUsername === undefined ? {} : { localUsername },
        }),
      ).then(validateLoginResult),
  });
}

/**
 * Adds a passkey right after a first sign-in through a provider, or right
 * after this device was signed in by another one. It does not go through
 * `runWithSudo`: that prompt lives in the console, and the new session is
 * fresh enough. A session that is not any more fails with
 * `sudo_required`, which the page answers itself.
 */
export function setupPasskeyMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "setup-signin" },
    mutationFn: () => registerPasskey(),
  });
}

/** Sets a password and an authenticator after a first federated sign-in. */
export function setupPasswordTotpMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    meta: { errorScope: "setup-signin" },
    mutationFn: (body: {
      password: string;
      secret_base32: string;
      code: string;
    }): Promise<RecoveryCodesResult> =>
      requireJsonData(
        client.POST("/api/prohibitorum/me/password-totp/verify", { body }),
      ),
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

/** Finishes signing in with a passkey picked from the username autofill. */
export function passkeyAutofillMutationOptions(returnTo?: string) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: (assertion: PasskeyAssertion) =>
      completePasskeyLogin({ assertion, mediation: "conditional", returnTo }),
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

/**
 * Every avatar write changes both the list of pictures and the session: the
 * sidebar draws the session's `avatarUrl`, which already carries a fresh query
 * string per version.
 */
export function invalidateAvatar(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({
      queryKey: myAvatarQueryOptions().queryKey,
    }),
    queryClient.invalidateQueries({ queryKey: ["session", "me"] }),
  ]);
}

/** Shows `user`, `none` or `upstream:<slug>`. */
export function selectAvatarMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.updateAvatar },
    retry: false,
    mutationFn: async (source: string) => {
      await client.PUT("/api/prohibitorum/me/avatar/selection", {
        body: { source },
      });
    },
    // A failed choice refreshes too: a source the server no longer has
    // (`avatar_source_unavailable`) drops out of the list.
    onSettled: () => invalidateAvatar(queryClient),
  });
}

/**
 * Uploads raw bytes, not a multipart form. The server shows the upload at once
 * only for an account that has never chosen a picture, so the pictures are
 * read again before the write counts as done, and the toast says which way it
 * went from that read.
 */
export function uploadAvatarMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: {
      success: (_file, avatar) =>
        (avatar as MyAvatarView).activeSource === "user"
          ? successMessage.updateAvatar
          : successMessage.uploadAvatar,
    },
    retry: false,
    mutationFn: async (file: File) => {
      await client.PUT("/api/prohibitorum/me/avatar", {
        body: file,
        // openapi-fetch serialises JSON by default; the endpoint wants the
        // bytes as they are.
        bodySerializer: (value) => value as BodyInit,
      });
      const [avatar] = await Promise.all([
        queryClient.fetchQuery({ ...myAvatarQueryOptions(), staleTime: 0 }),
        queryClient.invalidateQueries({ queryKey: ["session", "me"] }),
      ]);
      return avatar;
    },
  });
}

/** Where the picture in use falls back to is the server's decision. */
export function removeAvatarUploadMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removeAvatarUpload },
    retry: false,
    mutationFn: async () => {
      await client.DELETE("/api/prohibitorum/me/avatar");
    },
    onSuccess: () => invalidateAvatar(queryClient),
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
  await registerWithPasskey(
    () =>
      requireJsonData(
        client.POST("/api/prohibitorum/me/credentials/register/begin"),
      ),
    (attestation) =>
      client.POST("/api/prohibitorum/me/credentials/register/complete", {
        body: attestation,
        ...(nickname === undefined || nickname === ""
          ? {}
          : { params: { query: { nickname } } }),
      }),
  );
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
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/me/credentials/delete", {
            body: { id },
          }),
        sudoReason.deletePasskey,
      );
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
      runWithSudo(
        () =>
          requireJsonData(
            client.POST("/api/prohibitorum/me/password-totp/verify", { body }),
          ),
        sudoReason.setPasswordAndAuthenticator,
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
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/me/tokens/revoke", { body: { id } }),
        sudoReason.revokeToken,
      );
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["session", "tokens"] }),
  });
}

/* ------------------------------------------------- pairing a new device -- */

/** Starts a pairing: the code this device shows until another one approves it. */
export function pairingStartMutationOptions() {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: (): Promise<PairingStart> =>
      requireJsonData(client.POST("/api/prohibitorum/auth/devices/pair/begin")),
  });
}

/** The codes with which completing says the pairing is gone for good. */
const pairingGone = new Set([
  "pairing_expired",
  "pairing_not_found",
  "pairing_state",
]);

export type PairingCompletion =
  | { outcome: "signed-in"; redirect: string }
  | { outcome: "expired" };

/**
 * Signs this device in with a pairing another device approved. A pairing that
 * ran out or was used up in the meantime is an outcome rather than a failure:
 * the page shows it as expired and offers a new code, so there is nothing for
 * the toast to say.
 */
export function pairingCompleteMutationOptions(returnTo: string | undefined) {
  return mutationOptions({
    retry: false,
    gcTime: 0,
    mutationFn: async (pairingId: string): Promise<PairingCompletion> => {
      try {
        const { redirect } = validateLoginResult(
          await requireJsonData(
            client.POST("/api/prohibitorum/auth/devices/pair/complete", {
              params: {
                query: returnTo === undefined ? {} : { return_to: returnTo },
              },
              body: { pairingId },
            }),
          ),
        );
        return { outcome: "signed-in", redirect };
      } catch (error) {
        if (error instanceof ApiError && pairingGone.has(error.code ?? "")) {
          return { outcome: "expired" };
        }
        throw error;
      }
    },
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

/**
 * Declines a pairing, or revokes one already approved: the server deletes the
 * pairing either way, so only the line the toast says differs.
 */
export function cancelDeviceMutationOptions(
  queryClient: QueryClient,
  success: MessageDescriptor = successMessage.declineDevice,
) {
  return mutationOptions({
    meta: { success },
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
 * Management mutations. Sudo guards only the writes that remove something for
 * good or hand out a credential — deleting an account, a new registration
 * link, a change of role, and on the application pages deleting an
 * application or provider, a new client secret and the signing keys. Each of
 * those wraps *its own* call in `runWithSudo` rather than pushing the ceremony
 * up to the caller, so a page calls `mutate` and gets the step-up prompt as
 * part of the mutation itself; every other write calls the client directly.
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

/**
 * Replaces the whole account record: an omitted `attributes` clears them. Only
 * a change of role needs a fresh verification, so the caller passes the role
 * the account has now and a profile edit goes straight through.
 */
export function updateAccountMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveAccount },
    retry: false,
    mutationFn: async ({
      id,
      body,
      previousRole,
    }: {
      id: number;
      body: UpdateAccountInput;
      previousRole: UpdateAccountInput["role"];
    }): Promise<AccountView> => {
      const save = () =>
        requireJsonData(
          client.PUT("/api/prohibitorum/accounts/{id}", {
            params: { path: { id } },
            body,
          }),
        );
      return body.role === previousRole
        ? save()
        : runWithSudo(save, sudoReason.changeAccountRole);
    },
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
      requireJsonData(
        client.POST("/api/prohibitorum/accounts/set-disabled", { body }),
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
      await client.POST("/api/prohibitorum/accounts/credentials/delete", {
        body,
      });
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
      await client.POST("/api/prohibitorum/accounts/tokens/revoke", { body });
    },
    onSuccess: (_result, { accountId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "accounts", accountId, "tokens"],
      }),
  });
}

/** Ending one session is reversible housekeeping. */
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
      requireJsonData(
        client.POST("/api/prohibitorum/accounts/revoke-sessions", {
          body: { id },
        }),
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
      requireJsonData(client.POST("/api/prohibitorum/groups", { body })),
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
      requireJsonData(
        client.PUT("/api/prohibitorum/groups/{groupId}", {
          params: { path: { groupId } },
          body,
        }),
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
      await client.POST("/api/prohibitorum/groups/{groupId}/delete", {
        params: { path: { groupId } },
      });
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
      requireJsonData(
        client.POST("/api/prohibitorum/groups/{groupId}/decisions", {
          params: { path: { groupId } },
          body,
        }),
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
      await client.POST("/api/prohibitorum/groups/{groupId}/decisions/clear", {
        params: { path: { groupId } },
        body,
      });
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
      requireJsonData(client.POST("/api/prohibitorum/invitations", { body })),
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
      await client.POST("/api/prohibitorum/invitations/revoke", {
        body: { token },
      });
    },
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["admin", "invitations"] }),
  });
}

/* ------------------------------------------------------ instance settings -- */

/**
 * Every instance setting `/config` publishes — the name, the maintenance notice,
 * the icon and the sign-in page's look — is read from that one query, by the
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
      await client.PUT("/api/prohibitorum/admin/settings", {
        body: { instanceName },
      });
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
      await client.PUT("/api/prohibitorum/admin/settings/maintenance", {
        body,
      });
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

/** Uploads raw bytes, not a multipart form. */
export function uploadInstanceIconMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.updateInstanceIcon },
    retry: false,
    mutationFn: async (file: File) => {
      await client.PUT("/api/prohibitorum/admin/settings/icon", {
        body: file,
        // openapi-fetch serialises JSON by default; the endpoint wants the
        // bytes as they are.
        bodySerializer: (value) => value as BodyInit,
      });
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

export function removeInstanceIconMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removeInstanceIcon },
    retry: false,
    mutationFn: async () => {
      await client.DELETE("/api/prohibitorum/admin/settings/icon");
    },
    onSuccess: () => invalidatePublicConfig(queryClient),
  });
}

/**
 * The sign-in page's settings are read by the settings panel and, through
 * `/config`, by every public page; a write refreshes both.
 */
function invalidateSignInPage(queryClient: QueryClient) {
  return Promise.all([
    invalidatePublicConfig(queryClient),
    queryClient.invalidateQueries({
      queryKey: ["admin", "settings", "login-appearance"],
    }),
  ]);
}

/** Saves the whole appearance, and the Unsplash key when one was typed. */
export function updateLoginAppearanceMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveSignInPage },
    retry: false,
    mutationFn: async (body: LoginAppearanceWrite) => {
      await client.PUT("/api/prohibitorum/admin/settings/login-appearance", {
        body,
      });
    },
    onSuccess: () => invalidateSignInPage(queryClient),
  });
}

export function removeUnsplashKeyMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removeUnsplashKey },
    retry: false,
    mutationFn: async () => {
      await client.DELETE(
        "/api/prohibitorum/admin/settings/login-appearance/unsplash-key",
      );
    },
    onSuccess: () => invalidateSignInPage(queryClient),
  });
}

/** Uploads one image as raw bytes, like the icon. */
export function uploadLoginImageMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.addSignInImage },
    retry: false,
    mutationFn: (file: File) =>
      requireJsonData(
        client.POST("/api/prohibitorum/admin/settings/login-images", {
          body: file,
          bodySerializer: (value) => value as BodyInit,
        }),
      ),
    onSuccess: () => invalidateSignInPage(queryClient),
  });
}

export function removeLoginImageMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.removeSignInImage },
    retry: false,
    mutationFn: async (id: number) => {
      await client.DELETE(
        "/api/prohibitorum/admin/settings/login-images/{id}",
        {
          params: { path: { id } },
        },
      );
    },
    onSuccess: () => invalidateSignInPage(queryClient),
  });
}

/** Replaces the whole policy; the server keeps nothing from the previous one. */
export function updateClientIpMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    meta: { success: successMessage.saveClientIp },
    retry: false,
    mutationFn: async (body: ClientIpSettings) => {
      await client.PUT("/api/prohibitorum/admin/settings/client-ip", { body });
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
function invalidateIdentityProviders(queryClient: QueryClient) {
  // One call: a second, for the detail's own key, would cancel the detail's
  // refetch this one has just started, and the page reading it would fail.
  return queryClient.invalidateQueries({
    queryKey: ["admin", "identity-providers"],
  });
}

export function createIdentityProviderMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.createIdentityProvider },
    retry: false,
    mutationFn: async (body: ProviderWriteBody) =>
      requireJsonData(
        client.POST("/api/prohibitorum/identity-providers", { body }),
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
      requireJsonData(
        client.PUT("/api/prohibitorum/identity-providers/{slug}", {
          params: { path: { slug } },
          body,
        }),
      ),
    onSuccess: () => invalidateIdentityProviders(queryClient),
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
      await client.POST("/api/prohibitorum/identity-providers/rotate-secret", {
        body: { slug, secret },
      });
    },
    onSuccess: () => invalidateIdentityProviders(queryClient),
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
    onSuccess: () => invalidateIdentityProviders(queryClient),
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
 * The entity icons all share one shape: raw bytes on PUT and nothing on DELETE.
 * Either way the entity's own queries are invalidated, because its iconUrl — and the cache-buster in it — just changed.
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
    ? invalidateIdentityProviders(queryClient)
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
      if (target.kind === "identity-provider") {
        await client.PUT("/api/prohibitorum/identity-providers/{slug}/icon", {
          params: { path: { slug: target.slug } },
          ...options,
        });
      } else if (target.kind === "saml") {
        await client.PUT("/api/prohibitorum/saml-applications/{id}/icon", {
          params: { path: { id: Number(target.appId) } },
          ...options,
        });
      } else if (target.kind === "oidc") {
        await client.PUT(
          "/api/prohibitorum/oidc-applications/{clientId}/icon",
          {
            params: { path: { clientId: target.appId } },
            ...options,
          },
        );
      } else {
        await client.PUT(
          "/api/prohibitorum/forward-auth-apps/{clientId}/icon",
          {
            params: { path: { clientId: target.appId } },
            ...options,
          },
        );
      }
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
      if (target.kind === "identity-provider") {
        await client.DELETE(
          "/api/prohibitorum/identity-providers/{slug}/icon",
          {
            params: { path: { slug: target.slug } },
          },
        );
      } else if (target.kind === "saml") {
        await client.DELETE("/api/prohibitorum/saml-applications/{id}/icon", {
          params: { path: { id: Number(target.appId) } },
        });
      } else if (target.kind === "oidc") {
        await client.DELETE(
          "/api/prohibitorum/oidc-applications/{clientId}/icon",
          { params: { path: { clientId: target.appId } } },
        );
      } else {
        await client.DELETE(
          "/api/prohibitorum/forward-auth-apps/{clientId}/icon",
          {
            params: { path: { clientId: target.appId } },
          },
        );
      }
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
    meta: { errorScope: "diagnostic" },
    mutationFn: async (slug: string): Promise<DiagnosticStartView> =>
      requireJsonData(
        client.POST("/api/prohibitorum/identity-providers/{slug}/tests", {
          params: { path: { slug } },
        }),
      ),
  });
}

/** Marks the callback as received; the read that follows carries the result. */
export function completeDiagnosticMutationOptions(queryClient: QueryClient) {
  return mutationOptions({
    retry: false,
    meta: { errorScope: "diagnostic" },
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
          { params: { path: { slug, id } } },
        ),
      ),
    onSuccess: (_result, { slug, id }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "identity-providers", slug, "tests", id],
      }),
  });
}

/* -------------------------------------------------- VRChat operator session -- */

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
      requireJsonData(
        client.POST(
          "/api/prohibitorum/identity-providers/{slug}/operator-session/start",
          { params: { path: { slug } }, body },
        ),
      ),
    onSuccess: () => invalidateIdentityProviders(queryClient),
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
      requireJsonData(
        client.POST(
          "/api/prohibitorum/identity-providers/{slug}/operator-session/verify",
          { params: { path: { slug } }, body },
        ),
      ),
    onSuccess: () => invalidateIdentityProviders(queryClient),
  });
}

export function validateOperatorSessionMutationOptions(
  queryClient: QueryClient,
) {
  return mutationOptions({
    meta: { success: successMessage.validateOperatorSession },
    retry: false,
    mutationFn: async (slug: string): Promise<OperatorSessionView> =>
      requireJsonData(
        client.POST(
          "/api/prohibitorum/identity-providers/{slug}/operator-session/validate",
          { params: { path: { slug } } },
        ),
      ),
    onSuccess: () => invalidateIdentityProviders(queryClient),
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
      requireJsonData(
        client.POST("/api/prohibitorum/oidc-applications", { body }),
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
      requireJsonData(
        client.PUT("/api/prohibitorum/oidc-applications/{clientId}", {
          params: { path: { clientId } },
          body,
        }),
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
      requireJsonData(
        client.POST("/api/prohibitorum/forward-auth-apps", { body }),
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
      requireJsonData(
        client.PUT("/api/prohibitorum/forward-auth-apps/{clientId}", {
          params: { path: { clientId } },
          body,
        }),
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
      await runWithSudo(
        () =>
          client.POST("/api/prohibitorum/saml-applications/delete", {
            body: { id },
          }),
        sudoReason.deleteApplication,
      );
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
 * Assigning and removing are admin-only. The manager list is not
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
      (await kind) === "saml"
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
      (await kind) === "saml"
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
            );
    },
    onSuccess: (_result, { appId }) =>
      queryClient.invalidateQueries({
        queryKey: ["admin", "managed-applications", kind, appId, "managers"],
      }),
  });
}
