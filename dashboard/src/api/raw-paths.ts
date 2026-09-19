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
}
