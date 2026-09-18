export type AppKind = 'oidc' | 'forward_auth' | 'saml'
export type GroupKind = 'manual' | 'rule'
export type ManualEffect = 'allow' | 'deny'


export interface ProviderDescriptor {
  slug: string
  displayName: string
}
export interface AppScope {
  name: string
  description: string
}

export interface ManagedApplication {
  iconUrl?: string
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
  applicationCount?: number
}

export interface GroupApplication {
  iconUrl?: string
  kind: AppKind
  appId: string
  displayName: string
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
  providers: ProviderDescriptor[]
  groups: AppGroup[]
}


export interface GroupPreview {
  account: AccountSummary
  matched: boolean
}

export interface RulePreviewPage {
  items: GroupPreview[]
  matchedCount: number
  nextCursor: string
}

export interface ExplanationNode {
  path: string
  label: string
  result: boolean
  children?: ExplanationNode[]
}

export interface GroupExplanation {
  account: AccountSummary
  explanation: ExplanationNode
}
