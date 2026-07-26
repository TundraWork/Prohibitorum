export type AppKind = 'oidc' | 'forward_auth' | 'saml'
export type GroupKind = 'manual' | 'rule'
export type ManualEffect = 'allow' | 'deny'

export interface AppScope {
  name: string
  description: string
}

export interface ManagedApplication {
  kind: AppKind
  appId: string
  displayName: string
  launchUrl?: string
  redirectUris?: string[]
  entityId?: string
  forwardAuthHost?: string
  forwardAuthScopes?: AppScope[]
  accessRestricted: boolean
}

export interface Condition {
  op?: 'all' | 'any' | 'not'
  children?: Condition[]
  child?: Condition
  fact?: 'connection.provider' | 'connection.protocol' | 'login_method' | 'avatar'
  provider?: string
  protocol?: 'oidc' | 'steam' | 'vrchat'
  method?: 'passkey' | 'password_totp' | 'federation'
  source?: 'any' | 'user_uploaded'
}

export interface Rule {
  version: number
  condition: Condition
}

export interface AppGroup {
  id: number
  kind: GroupKind
  slug: string
  displayName: string
  description?: string
  exposedToDownstream: boolean
  rule?: Rule
}

export interface AccountSummary {
  id: number
  username: string
  displayName: string
}

export interface ManualDecision {
  account: AccountSummary
  effect: ManualEffect
  updatedAt: string
}

export interface AppAccessWorkspace {
  app: ManagedApplication
  accessRestricted: boolean
  manualGroup?: AppGroup
  ruleGroups: AppGroup[]
}

