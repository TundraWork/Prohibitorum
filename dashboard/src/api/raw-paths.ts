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
  hasCustomBackground: boolean;
  backgroundUrl: string;
  backgroundEtag: string;
  totp: {
    issuer: string;
    algorithm: string;
    digits: number;
    period: number;
  };
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

export interface CreatedPersonalAccessToken {
  token: string;
  pat: {
    id: number;
    name: string;
    tokenHint: string;
    allApps: boolean;
    appGrants: Record<string, string[] | null>;
    createdAt: string;
    expiresAt?: string;
    lastUsedAt?: string;
  };
}

export interface RawPaths {
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
        query?: never;
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
        query?: { return_to?: string };
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
}
