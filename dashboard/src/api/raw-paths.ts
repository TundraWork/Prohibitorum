import type {
  AuthenticationResponseJSON,
  PublicKeyCredentialRequestOptionsJSON,
} from "@simplewebauthn/browser";

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
}
