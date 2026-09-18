import { queryOptions } from '@tanstack/vue-query'
import { api } from '@/lib/api'
import type { AppKind } from '@/lib/appAccess'
import { buildPagePath, type Page } from '@/lib/pagination'
const collections: Record<AppKind, string> = { oidc: 'oidc-applications', saml: 'saml-applications', forward_auth: 'forward-auth-apps' }
export const managerQuery = <T>(kind: AppKind, appId: string) => queryOptions({
  queryKey: ['session', 'access', kind, appId, 'managers'], staleTime: 0,
  queryFn: ({ signal }) => api.get<T>('/api/prohibitorum/' + collections[kind] + '/' + encodeURIComponent(appId) + '/managers', { signal }),
})
export const accessQuery = <T>(kind: AppKind, appId: string, section: 'access' | 'accounts', cursor = '') => queryOptions({
  queryKey: ['session', 'access', kind, appId, section, { cursor }], staleTime: 0,
  queryFn: ({ signal }) => api.get<T>(buildPagePath('/api/prohibitorum/managed-applications/' + kind + '/' + encodeURIComponent(appId) + '/' + section, { cursor }), { signal }),
})
export const groupQuery = <T>(kind: AppKind, appId: string, groupId: number, section: 'decisions' | 'preview' | 'explain', parameters: { cursor?: string; accountId?: number } = {}) => queryOptions({
  queryKey: ['session', 'access', kind, appId, 'groups', groupId, section, parameters], staleTime: 0,
  queryFn: ({ signal }) => api.get<T>(buildPagePath('/api/prohibitorum/managed-applications/' + kind + '/' + encodeURIComponent(appId) + '/groups/' + groupId + '/' + section + (section === 'explain' ? '/' + parameters.accountId : ''), section === 'explain' ? {} : parameters), { signal }),
})
export const allAccessAccountsQuery = <T>(kind: AppKind, appId: string) => queryOptions({
  queryKey: ['session', 'access', kind, appId, 'all-accounts'], staleTime: 0,
  queryFn: async ({ signal }) => {
    const items: T[] = [], seen = new Set<string>(); let cursor = ''
    do {
      seen.add(cursor)
      const page = await api.get<Page<T>>(buildPagePath('/api/prohibitorum/managed-applications/' + kind + '/' + encodeURIComponent(appId) + '/accounts', { cursor }), { signal })
      items.push(...page.items); cursor = page.nextCursor
    } while (cursor && !seen.has(cursor))
    return items
  },
})
export const allDecisionsQuery = <T>(kind: AppKind, appId: string, groupId: number) => queryOptions({
  queryKey: ['session', 'access', kind, appId, 'groups', groupId, 'all-decisions'], staleTime: 0,
  queryFn: async ({ signal }) => {
    const items: T[] = [], seen = new Set<string>(); let cursor = ''
    do {
      seen.add(cursor)
      const page = await api.get<Page<T>>(buildPagePath('/api/prohibitorum/managed-applications/' + kind + '/' + encodeURIComponent(appId) + '/groups/' + groupId + '/decisions', { cursor }), { signal })
      items.push(...page.items); cursor = page.nextCursor
    } while (cursor && !seen.has(cursor))
    return items
  },
})
