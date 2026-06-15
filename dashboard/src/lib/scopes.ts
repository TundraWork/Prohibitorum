/**
 * Common upstream OIDC scope suggestions, as data only (no vue-i18n import here).
 * Each entry pairs a scope `value` with an `descKey` i18n key resolved by the caller,
 * e.g. UPSTREAM_SCOPE_SUGGESTIONS.map(s => ({ value: s.value, description: t(s.descKey) })).
 */
export interface ScopeSuggestion {
  value: string
  descKey: string
}

export const UPSTREAM_SCOPE_SUGGESTIONS: ScopeSuggestion[] = [
  { value: 'openid', descKey: 'admin.upstream.scopeSuggestions.openid' },
  { value: 'profile', descKey: 'admin.upstream.scopeSuggestions.profile' },
  { value: 'email', descKey: 'admin.upstream.scopeSuggestions.email' },
  { value: 'offline_access', descKey: 'admin.upstream.scopeSuggestions.offline_access' },
  { value: 'address', descKey: 'admin.upstream.scopeSuggestions.address' },
  { value: 'phone', descKey: 'admin.upstream.scopeSuggestions.phone' },
  { value: 'groups', descKey: 'admin.upstream.scopeSuggestions.groups' },
]

/**
 * Fixed OIDC scope set for the ScopeSelector component (OIDC mode).
 * openid is always required; the rest are optional.
 * Descriptions reuse the upstream.scopeSuggestions keys (same scope set, same text).
 */
export interface OidcScope {
  value: string
  required: boolean
  descKey: string
}

export const OIDC_SCOPES: OidcScope[] = [
  { value: 'openid',         required: true,  descKey: 'admin.upstream.scopeSuggestions.openid' },
  { value: 'profile',        required: false, descKey: 'admin.upstream.scopeSuggestions.profile' },
  { value: 'email',          required: false, descKey: 'admin.upstream.scopeSuggestions.email' },
  { value: 'offline_access', required: false, descKey: 'admin.upstream.scopeSuggestions.offline_access' },
]
