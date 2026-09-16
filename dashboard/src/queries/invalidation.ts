import type { QueryClient, QueryKey } from '@tanstack/vue-query'
import { keys, type Collection } from './resources'

export type MutationResource = Collection | 'profile' | 'avatar' | 'consent' | 'identities' | 'credentials' | 'sessions' | 'tokens' | 'settings' | 'access'
const projections: Partial<Record<MutationResource, QueryKey[]>> = {
  profile: [keys.me, ['session', 'accounts']], avatar: [keys.me, ['session', 'accounts']],
  consent: [keys.member('consent'), keys.member('apps')],
  identities: [keys.member('identities'), keys.member('apps'), keys.me],
  credentials: [keys.member('credentials'), keys.member('factors'), keys.member('sessions'), ['session', 'accounts']],
  sessions: [keys.member('sessions'), ['session', 'accounts']],
  tokens: [keys.member('tokens'), ['session', 'accounts']],
  settings: [keys.config, ['session', 'settings']],
  'identity-providers': [keys.federation, keys.member('identities')],
  accounts: [keys.me, ['session', 'access'], ['session', 'me', 'credentials'], ['session', 'me', 'sessions'], ['session', 'me', 'tokens']],
  access: [['session', 'access'], ['session', 'accounts'], ['session', 'managed-applications'], keys.member('apps'), keys.member('consent'), keys.member('forward-auth-apps')],
}
export async function invalidateResource(client: QueryClient, resource: MutationResource): Promise<void> {
  const affected: QueryKey[] = [['session', resource], ...(projections[resource] ?? [])]
  if (['oidc-applications', 'saml-applications', 'forward-auth-apps'].includes(resource)) {
    affected.push(keys.member('apps'), keys.member('consent'), keys.member('forward-auth-apps'), ['session', 'managed-applications'])
  }
  const unique = affected.filter((key, index) => affected.findIndex(other => JSON.stringify(other) === JSON.stringify(key)) === index)
  await Promise.all(unique.map(queryKey => client.invalidateQueries({ queryKey })))
}

/** Remove a deleted detail and its sections before related list invalidation. */
export async function removeDetail(client: QueryClient, resource: Collection, id: string | number): Promise<void> {
  const queryKey = keys.detail(resource, id)
  await client.cancelQueries({ queryKey })
  for (const query of client.getQueryCache().findAll({ queryKey })) query.reset()
  client.removeQueries({ queryKey })
}
