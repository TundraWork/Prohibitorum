import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
  RegistrationResponseJSON,
} from "@simplewebauthn/browser";
import type { components } from "@/api/generated/schema";

export interface PublicConfig {
  instanceName: string;
  hasCustomIcon: boolean;
  iconUrl: string;
  iconEtag: string;
  maintenanceMode: boolean;
  maintenanceMessage: string;
  loginAppearance: LoginAppearance;
  /** The uploaded sign-in backgrounds in upload order, whichever source is chosen. */
  loginImages: LoginImage[];
  totp: {
    issuer: string;
    algorithm: string;
    digits: number;
    period: number;
  };
}

export type LoginBackgroundSource =
  | "none"
  | "color"
  | "gradient"
  | "bing"
  | "unsplash"
  | "images";

export type LoginGradient =
  | "dawn"
  | "lagoon"
  | "aurora"
  | "dusk"
  | "mist"
  | "ember";

export type LoginImageOrder = "random" | "carousel";

/** How the sign-in card or the toolbar capsules sit over the background. */
export interface LoginSurface {
  translucent: boolean;
  /** 0–100, applied only while translucent. */
  opacity: number;
  blur: boolean;
}

/**
 * The sign-in page's look. Every field is always present, whichever source is
 * selected, so switching source keeps the others' settings. The server rejects
 * a document with a field missing or added.
 */
export interface LoginAppearance {
  background: {
    source: LoginBackgroundSource;
    /** Lowercase `#rrggbb`. */
    color: string;
    gradient: LoginGradient;
    bing: { market: string; showCaption: boolean };
    /** 0–64 characters, no surrounding whitespace. */
    unsplash: { query: string };
    /** `intervalSeconds` is 5–3600. */
    images: { order: LoginImageOrder; intervalSeconds: number };
  };
  card: LoginSurface;
  capsules: LoginSurface;
}

export interface LoginImage {
  id: number;
  url: string;
  etag: string;
}

/**
 * `GET /branding/wallpaper`: a picture the browser loads straight from Bing or
 * Unsplash, with its credit. Bing's title and copyright are left out when the
 * caption is hidden; Unsplash's credit is always there.
 */
export interface Wallpaper {
  source: "bing" | "unsplash";
  imageUrl: string;
  title?: string;
  copyright?: string;
  copyrightUrl?: string;
  photographer?: string;
  photographerUrl?: string;
  photoUrl?: string;
}

export interface PasswordRequest {
  username: string;
  password: string;
}

export interface PasswordResult {
  partial_session_token: string;
}

export interface TotpRequest {
  partial_session_token: string;
  code: string;
}

export type RecoveryRequest =
  | {
      partial_session_token: string;
      code: string;
      reset_authenticator: false;
      totp_secret_base32?: never;
      totp_code?: never;
    }
  | {
      partial_session_token: string;
      code: string;
      reset_authenticator: true;
      totp_secret_base32: string;
      totp_code: string;
    };

export interface LoginResult {
  redirect: string;
}

export type RecoveryResult =
  | (LoginResult & { recovery_codes?: never })
  | (LoginResult & { recovery_codes: string[] });

/** Local second-factor methods the account can use to refresh its sudo window. */
export type SudoMethod = "webauthn" | "password_totp";

export interface SudoMethods {
  methods: SudoMethod[];
  fresh: boolean;
}

export interface SudoPasswordTotpComplete {
  current_password: string;
  totp_code: string;
}

/** `POST /me/credentials/register/complete` returns the created credential. */
export interface CreatedCredential {
  id: number;
  nickname?: string;
  createdAt: string;
  lastUsedAt?: string;
  backupState: boolean;
  attestationType: string;
  credentialIdSuffix: string;
  transports: string[] | null;
}

export interface RecoveryCodesResult {
  recovery_codes: string[];
}

export interface AvatarStatus {
  pending: boolean;
}

export interface DevicePairing {
  pairingId: string;
  displayCode: string;
  initiatorUa: string;
  initiatorIp: string;
  createdAt: string;
  expiresAt: string;
  alreadyBound: boolean;
}

/** `POST /auth/devices/pair/begin`: the code a new device shows while it waits. */
export interface PairingStart {
  pairingId: string;
  /** The raw 8-character code, which the approval link carries. */
  code: string;
  /** The code as the reader sees it, `XXXX-XXXX`. */
  displayCode: string;
  expiresAt: string;
}

/**
 * `GET /auth/devices/pair/status`: where a pairing stands. A pairing that is
 * used up or unknown reads as `expired`.
 */
export interface PairingStatus {
  status: "pending" | "approved" | "expired";
  expiresAt?: string;
}

/** `POST /auth/devices/pair/complete`: the new device's session and where it goes. */
export interface PairingComplete {
  session: components["schemas"]["SessionView"];
  redirect: string;
}

export interface CreatedPersonalAccessToken {
  token: string;
  pat: {
    id: number;
    name: string;
    tokenHint: string;
    access: "selected_apps" | "all_apps" | "full" | "sudo";
    apps: { clientId: string; displayName: string }[] | null;
    createdAt: string;
    expiresAt?: string;
    lastUsedAt?: string;
  };
}

export interface ConsentAccount {
  displayName: string;
  avatarUrl?: string;
}

/** `GET /consent`: the OIDC authorization waiting on the signed-in account. */
export interface ConsentRequest {
  client: {
    clientId: string;
    displayName: string;
    logoUri?: string;
    policyUri?: string;
    tosUri?: string;
  };
  account: ConsentAccount;
  scopes: string[];
  /** The requested scopes this account has allowed before; absent the first time. */
  alreadyGranted?: string[];
}

/** `GET /saml-consent`: the SAML sign-in waiting on the signed-in account. */
export interface SamlConsentRequest {
  sp: { id: string; displayName: string; logoUri?: string };
  account: ConsentAccount;
  /** Labels of the attributes the service will receive. */
  attributes: string[];
}

/** Where the browser goes once a consent decision is made. */
export interface ConsentDecision {
  redirect: string;
}

type SessionView = components["schemas"]["SessionView"];

/** The account fields an enrollment sends with its first request. */
export interface EnrollmentAccountFields {
  username?: string;
  displayName?: string;
}

/** `POST /enrollments/{token}/register/complete`: the new account is signed in. */
export interface EnrollmentRegistered {
  session: SessionView;
  newCredentialId: number;
}

/**
 * `POST /enrollments/{token}/password-totp/verify`. The field is
 * `recoveryCodes` here, unlike `recovery_codes` on `/me/password-totp/verify`.
 */
export interface EnrollmentPasswordTotpResult {
  session: SessionView;
  recoveryCodes: string[];
}

/** `GET /auth/federation/confirm`: the account a first federated sign-in prepared. */
export interface FederationConfirm {
  idpDisplayName: string;
  displayName: string;
  username: string;
  email: string;
  avatarUrl?: string;
  /** The picture is still being fetched from the provider. */
  avatarPending: boolean;
}

/** `POST /auth/federation/confirm`: where to go, and whether to offer a local sign-in. */
export interface FederationConfirmResult {
  redirect: string;
  offerLocalSignin: boolean;
}

export type FederationFlowIntent = "login" | "link" | "invite" | "enroll";
export type FederationFlowStep = "identify" | "proof";

/**
 * A VRChat profile verification in progress, as the page reads it after
 * `readFederationFlow` has checked the loose fields.
 */
export interface FederationFlow {
  provider: { slug: string; displayName: string; protocol: string };
  intent: FederationFlowIntent;
  step: FederationFlowStep;
  profileUrl?: string;
  proofUrl?: string;
  requiresLocalUsername: boolean;
  expiresAt: string;
}

export interface RawPaths {
  "/branding/wallpaper": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": Wallpaper } };
      };
    };
  };
  "/api/prohibitorum/config": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PublicConfig } };
      };
    };
  };
  "/api/prohibitorum/auth/logout": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/auth/password/begin": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": PasswordRequest } };
      responses: {
        200: { content: { "application/json": PasswordResult } };
      };
    };
  };
  "/api/prohibitorum/auth/totp/verify": {
    post: {
      parameters: {
        query?: { return_to?: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": TotpRequest } };
      responses: {
        200: { content: { "application/json": LoginResult } };
      };
    };
  };
  "/api/prohibitorum/auth/recovery-code/verify": {
    post: {
      parameters: {
        query?: { return_to?: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": RecoveryRequest } };
      responses: {
        200: { content: { "application/json": RecoveryResult } };
      };
    };
  };
  "/api/prohibitorum/auth/login/begin": {
    post: {
      parameters: {
        query?: { mediation?: "conditional" };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: {
          content: {
            "application/json": PublicKeyCredentialRequestOptionsJSON;
          };
        };
      };
    };
  };
  "/api/prohibitorum/auth/login/complete": {
    post: {
      parameters: {
        query?: { mediation?: "conditional"; return_to?: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": AuthenticationResponseJSON };
      };
      responses: {
        200: { content: { "application/json": LoginResult } };
      };
    };
  };
  "/api/prohibitorum/me/sudo/methods": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": SudoMethods } };
      };
    };
  };
  "/api/prohibitorum/me/sudo/begin": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": {
            method: SudoMethod;
            slug?: string;
            returnTo?: string;
          };
        };
      };
      responses: {
        /** password_totp answers 204 with no challenge and no body. */
        204: { content?: never };
        200: {
          content: {
            "application/json": PublicKeyCredentialRequestOptionsJSON;
          };
        };
      };
    };
  };
  "/api/prohibitorum/me/sudo/complete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      /** The body shape follows whichever method /begin selected. */
      requestBody: {
        content: {
          "application/json":
            | AuthenticationResponseJSON
            | SudoPasswordTotpComplete;
        };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/avatar": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      /** Raw image bytes, not multipart. */
      requestBody: { content: { "application/octet-stream": Blob } };
      responses: { 204: { content?: never } };
    };
    delete: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/avatar/selection": {
    put: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": { source: string } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/avatar/status": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": AvatarStatus } };
      };
    };
  };
  "/api/prohibitorum/me/credentials/register/begin": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: {
          content: {
            "application/json": PublicKeyCredentialCreationOptionsJSON;
          };
        };
      };
    };
  };
  "/api/prohibitorum/me/credentials/register/complete": {
    post: {
      parameters: {
        query?: { nickname?: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RegistrationResponseJSON };
      };
      responses: {
        200: { content: { "application/json": CreatedCredential } };
      };
    };
  };
  "/api/prohibitorum/me/password/set": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": { password: string } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/password-totp/verify": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": {
            password: string;
            secret_base32: string;
            code: string;
          };
        };
      };
      responses: {
        200: { content: { "application/json": RecoveryCodesResult } };
      };
    };
  };
  "/api/prohibitorum/me/totp/verify": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": { secret_base32: string; code: string };
        };
      };
      responses: {
        200: { content: { "application/json": RecoveryCodesResult } };
      };
    };
  };
  "/api/prohibitorum/me/recovery-codes/regenerate": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": RecoveryCodesResult } };
      };
    };
  };
  "/api/prohibitorum/me/auth/revoke-password-totp": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/identities": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: {
          content: {
            "application/json":
              | components["schemas"]["AccountIdentityView"][]
              | null;
          };
        };
      };
    };
  };
  "/api/prohibitorum/me/identities/{id}/unlink": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/devices/pair/lookup": {
    get: {
      parameters: {
        query: { code: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": DevicePairing } };
      };
    };
  };
  "/api/prohibitorum/me/devices/pair/approve": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": { code: string } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/me/devices/pair/cancel": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": { code: string } } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/auth/devices/pair/begin": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PairingStart } };
      };
    };
  };
  "/api/prohibitorum/auth/devices/pair/status": {
    get: {
      parameters: {
        query: { id: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PairingStatus } };
      };
    };
  };
  "/api/prohibitorum/auth/devices/pair/complete": {
    post: {
      parameters: {
        query?: { return_to?: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": { pairingId: string } } };
      responses: {
        200: { content: { "application/json": PairingComplete } };
      };
    };
  };
  "/api/prohibitorum/auth/federation": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: {
          content: {
            "application/json":
              | components["schemas"]["FederationProvider"][]
              | null;
          };
        };
      };
    };
  };
  "/api/prohibitorum/consent": {
    get: {
      parameters: {
        query: { ticket: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": ConsentRequest } };
      };
    };
    post: {
      parameters: {
        query: { return_to: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": { ticket: string; decision: "approve" | "deny" };
        };
      };
      responses: {
        200: { content: { "application/json": ConsentDecision } };
      };
    };
  };
  "/api/prohibitorum/saml-consent": {
    get: {
      parameters: {
        query: { ticket: string };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": SamlConsentRequest } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": {
            ticket: string;
            decision: "approve" | "decline";
          };
        };
      };
      responses: {
        200: { content: { "application/json": ConsentDecision } };
      };
    };
  };
  "/api/prohibitorum/enrollments/{token}/register/begin": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { token: string };
        cookie?: never;
      };
      /** Absent for a reset, which keeps the account it names. */
      requestBody?: {
        content: { "application/json": EnrollmentAccountFields };
      };
      responses: {
        200: {
          content: {
            "application/json": PublicKeyCredentialCreationOptionsJSON;
          };
        };
      };
    };
  };
  "/api/prohibitorum/enrollments/{token}/register/complete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { token: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RegistrationResponseJSON };
      };
      responses: {
        200: { content: { "application/json": EnrollmentRegistered } };
      };
    };
  };
  "/api/prohibitorum/enrollments/{token}/password-totp/verify": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { token: string };
        cookie?: never;
      };
      requestBody: {
        content: {
          "application/json": EnrollmentAccountFields & {
            password: string;
            secret_base32: string;
            code: string;
          };
        };
      };
      responses: {
        200: {
          content: { "application/json": EnrollmentPasswordTotpResult };
        };
      };
    };
  };
  "/api/prohibitorum/auth/federation/confirm": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": FederationConfirm } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": Record<string, never> };
      };
      responses: {
        200: { content: { "application/json": FederationConfirmResult } };
      };
    };
  };
  "/api/prohibitorum/auth/federation/confirm/decline": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/auth/federation/flows/{flow}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { flow: string };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        /** Loose until `readFederationFlow` has checked it. */
        200: { content: { "application/json": unknown } };
      };
    };
  };
  "/api/prohibitorum/auth/federation/flows/{flow}/prepare": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { flow: string };
        cookie?: never;
      };
      requestBody: { content: { "application/json": { identity: string } } };
      responses: {
        200: { content: { "application/json": unknown } };
      };
    };
  };
  "/api/prohibitorum/auth/federation/flows/{flow}/verify": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { flow: string };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": { localUsername?: string } };
      };
      responses: {
        200: { content: { "application/json": LoginResult } };
      };
    };
  };
}
