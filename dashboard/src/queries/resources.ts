import { queryOptions, type QueryClient } from '@tanstack/vue-query'
import { api } from '@/lib/api'
import { buildPagePath, type Page } from '@/lib/pagination'

export interface SessionView {
  id: number; username: string; displayName: string; role: string; attributes?: Record<string, unknown>
  avatarUrl?: string; avatarPending?: boolean; avatarSource?: string
  avatarSourceUrls?: Record<string, string>; avatarSourceLabels?: Record<string, string>
}
export interface PublicConfig {
  instanceName: string; hasCustomIcon: boolean; iconUrl: string; iconEtag: string
  maintenanceMode: boolean; maintenanceMessage: string
  hasCustomBackground: boolean; backgroundUrl: string; backgroundEtag: string
}
export const keys = {
  me: ['session', 'me', 'detail'] as const,
  config: ['public', 'config'] as const,
  federation: ['public', 'federation'] as const,
  collection: (resource: Collection, filters: PageFilters = {}) => ['session', resource, 'list', filters] as const,
  detail: (resource: Collection, id: string | number) => ['session', resource, 'detail', id] as const,
  member: (resource: MemberResource) => ['session', 'me', resource] as const,
}
export type Collection = 'accounts' | 'identity-providers' | 'oidc-applications' | 'saml-applications' | 'forward-auth-apps' | 'invitations' | 'signing-keys' | 'audit-events'
export type MemberResource = 'apps' | 'consent' | 'identities' | 'credentials' | 'sessions' | 'tokens' | 'factors' | 'forward-auth-apps' | 'avatar/status' | 'sudo/methods'
export interface PageFilters {
  cursor?: string; limit?: number; q?: string; provider?: string; field?: string; match?: string; value?: string
  factor?: string; event?: string; accountId?: string; since?: string; until?: string
}
const prefix = '/api/prohibitorum/'
export const sessionQuery = () => queryOptions({
  queryKey: keys.me, staleTime: 0,
  queryFn: async ({ signal }): Promise<SessionView | null> => {
    try { return await api.get<SessionView>(prefix + 'me', { signal }) }
    catch (error) { if ((error as { code?: string }).code === 'no_session') return null; throw error }
  },
})
export const configQuery = () => queryOptions({ queryKey: keys.config, queryFn: ({ signal }) => api.get<PublicConfig>(prefix + 'config', { signal }) })
export const collectionQuery = <T>(resource: Collection, filters: PageFilters = {}) => queryOptions({
  queryKey: keys.collection(resource, filters),
  queryFn: ({ signal }) => api.get<Page<T>>(buildPagePath(prefix + resource, { ...filters }), { signal }),
})
export const detailQuery = <T>(resource: Collection, id: string | number) => queryOptions({
  queryKey: keys.detail(resource, id),
  queryFn: ({ signal }) => api.get<T>(prefix + resource + '/' + encodeURIComponent(id), { signal }),
})
export const memberQuery = <T>(resource: MemberResource) => queryOptions({
  queryKey: keys.member(resource), staleTime: resource === 'sudo/methods' || resource === 'factors' ? 0 : 30_000,
  queryFn: ({ signal }) => api.get<T>(prefix + 'me/' + resource, { signal }),
})
export const federationQuery = <T>() => queryOptions({ queryKey: keys.federation, queryFn: ({ signal }) => api.get<T>(prefix + 'auth/federation', { signal }) })

export const managedApplicationsQuery = <T>() => queryOptions({ queryKey: ['session', 'managed-applications'], staleTime: 0, queryFn: ({ signal }) => api.get<T>(prefix + 'managed-applications', { signal }) })

export const accountSectionQuery = <T>(id: number, section: 'credentials' | 'sessions' | 'tokens' | 'identities') => queryOptions({ queryKey: ['session', 'accounts', 'detail', id, section], staleTime: 0, queryFn: ({ signal }) => api.get<T>(buildPagePath(prefix + 'accounts/' + id + '/' + section, section === 'identities' ? {} : { limit: 100 }), { signal }) })

export const settingsQuery = <T>(section: 'client-ip') => queryOptions({ queryKey: ['session', 'settings', section], queryFn: ({ signal }) => api.get<T>(prefix + 'admin/settings/' + section, { signal }) })

export const avatarStatusQuery = (client: QueryClient) => queryOptions({
  ...memberQuery<{ pending: boolean }>('avatar/status'),
  queryFn: async ({ signal }) => {
    const result = await api.get<{ pending: boolean }>(prefix + 'me/avatar/status', { signal })
    if (!result.pending && !signal.aborted) await client.invalidateQueries({ queryKey: keys.me })
    return result
  },
})
