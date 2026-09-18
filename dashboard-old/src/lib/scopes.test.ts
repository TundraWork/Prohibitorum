import { describe, it, expect } from 'vitest'
import { OIDC_SCOPES } from './scopes'
import en from '@/locales/en'

/**
 * Drift guard: every downstream OIDC scope the OP offers + honors must have a
 * human-readable consent description. Without it, ConsentScopeList falls back to
 * rendering the raw token as a "Custom permission requested by this application"
 * — which is wrong for a platform-defined scope (exactly how `groups`
 * regressed). The backend's closed vocabulary (oidc.SupportedScopes) is the
 * source of truth; this keeps the consent copy from lagging it.
 */
describe('downstream OIDC scopes have consent descriptions', () => {
  const consentScopes = en.consent.scopes as Record<string, string>
  for (const scope of OIDC_SCOPES) {
    it(`consent.scopes.${scope.value} exists`, () => {
      expect(consentScopes[scope.value], `missing consent.scopes.${scope.value}`).toBeTruthy()
    })
  }
})
