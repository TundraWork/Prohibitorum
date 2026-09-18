export interface OIDCConnectionConfig {
  configurationMode: 'discovery' | 'manual'
  endpoints: { authorization: string | null; token: string | null; userinfo: string | null; jwks: string | null }
  tokenAuthMethod: 'discovery' | 'client_secret_basic' | 'client_secret_post' | 'none'
  pkceMethod: 'S256' | 'plain' | 'off'
}

export function defaultOIDCConnection(): OIDCConnectionConfig {
  return { configurationMode: 'discovery', endpoints: { authorization: null, token: null, userinfo: null, jwks: null }, tokenAuthMethod: 'discovery', pkceMethod: 'S256' }
}

export function oidcConnectionError(config: OIDCConnectionConfig): string | null {
  if (config.configurationMode === 'manual' && (config.tokenAuthMethod === 'discovery' || !config.endpoints.authorization || !config.endpoints.token || !config.endpoints.jwks)) return 'admin.upstream.manualRequired'
  if (config.tokenAuthMethod === 'none' && config.pkceMethod !== 'S256') return 'admin.upstream.publicPKCE'
  for (const value of Object.values(config.endpoints)) {
    if (value === null) continue
    try {
      const url = new URL(value)
      if (value.trim() !== value || !['http:', 'https:'].includes(url.protocol) || !url.hostname || url.username || url.password || url.hash || /%(?![0-9a-f]{2})/i.test(url.search)) return 'admin.upstream.endpointInvalid'
    } catch { return 'admin.upstream.endpointInvalid' }
  }
  return null
}
