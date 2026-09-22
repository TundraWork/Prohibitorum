import type { components } from "@/api/generated/schema";

/**
 * Management writes and the whole user-group resource, hand-written because the
 * OpenAPI schema does not cover them: Huma registers the admin *reads* and four
 * account writes, but every other admin route is a bare chi handler, and the
 * user-group resource is bare chi throughout (see `api.md`). `raw-paths.ts`
 * stays what it is — sign-in and session — so the two remain separable.
 *
 * Shapes here are taken from the Go handlers, not from `api.md`, wherever the
 * two differ; the divergences are noted on the affected entries.
 */

/** `POST /invitations` — the account exists only once the invite is consumed. */
export interface CreateInvitationRequest {
  role: string;
  attributes?: Record<string, unknown>;
  /** A disabled or unknown slug is rejected at create, not at redemption. */
  expectedUpstreamIdpSlug?: string;
  username?: string;
  groupIds?: number[];
}

/** `POST /invitations/revoke` — by opaque bearer token, never by index. */
export interface RevokeInvitationRequest {
  token: string;
}

/** `POST /accounts/set-disabled` — flips only the flag, never the profile. */
export interface SetAccountDisabledRequest {
  id: number;
  disabled: boolean;
}

/** `POST /accounts/credentials/delete` — force-revoke someone else's passkey. */
export interface DeleteAccountCredentialRequest {
  accountId: number;
  credentialId: number;
}

/** `POST /accounts/tokens/revoke` — force-revoke a personal access token. */
export interface RevokeAccountTokenRequest {
  id: number;
}

/** `POST /accounts/{id}/sessions/revoke` — one session of one account. */
export interface RevokeAccountSessionRequest {
  id: string;
}

/** `POST /accounts/revoke-sessions` — every session of one account. */
export interface RevokeAccountSessionsRequest {
  id: number;
}

export interface RevokeAccountSessionsResult {
  revoked: number;
}

/** One entry of `GET /groups/providers`; the rule editor's provider vocabulary. */
export interface ProviderDescriptorView {
  slug: string;
  displayName: string;
}

/**
 * One rule AST node, mirroring `appaccess.Condition`. Exactly one shape is ever
 * populated: a combinator (`op` plus `children` for `all`/`any`, or `child` for
 * `not`), or a leaf (`fact` plus the one value its kind uses). A `not` holds a
 * leaf and never another combinator.
 */
export interface AppAccessCondition {
  op?: string;
  children?: AppAccessCondition[];
  child?: AppAccessCondition;
  fact?: string;
  provider?: string;
  protocol?: string;
  method?: string;
  source?: string;
}

export interface AppAccessRule {
  version: number;
  condition: AppAccessCondition;
}

/**
 * One reusable global policy group. `rule` is present only for `kind: "rule"`,
 * and `applicationCount` only for an admin caller — `GET /groups` answers a
 * non-admin with their own memberships, which carry neither. `description` is
 * omitted when empty.
 */
export interface AppGroupView {
  id: number;
  kind: string;
  slug: string;
  displayName: string;
  description?: string;
  exposedToDownstream: boolean;
  rule?: AppAccessRule;
  applicationCount?: number;
}

export interface CreateGroupRequest {
  kind: string;
  slug: string;
  displayName: string;
  description: string;
  exposedToDownstream?: boolean;
  rule?: AppAccessRule;
}

/** `kind` is rejected on update: a group's kind is fixed once created. */
export interface UpdateGroupRequest {
  slug: string;
  displayName: string;
  description: string;
  exposedToDownstream?: boolean;
  /** Omitted means "keep the current rule"; a manual group must never send one. */
  rule?: AppAccessRule;
}

/** The account fields a policy screen may see: no identity facts, no secrets. */
export interface AccountSummaryView {
  id: number;
  username: string;
  displayName: string;
}

/** `POST /groups/{groupId}/decisions` — write one allow or deny. */
export interface UpsertDecisionRequest {
  accountId: number;
  effect: "allow" | "deny";
}

/** `POST /groups/{groupId}/decisions/clear` — drop one account's decision. */
export interface ClearDecisionRequest {
  accountId: number;
}

/** One per-account allow or deny in a manual group. */
export interface ManualDecisionView {
  account: AccountSummaryView;
  effect: string;
  updatedAt: string;
}

/** One account that a rule group matches, or does not. */
export interface GroupPreviewView {
  account: AccountSummaryView;
  matched: boolean;
}

export interface PageManualDecisionView {
  items: ManualDecisionView[] | null;
  nextCursor: string;
}

export interface PageGroupPreviewView {
  items: GroupPreviewView[] | null;
  nextCursor: string;
}

/** A bounded condition-result tree; it never carries the account's own facts. */
export interface ExplanationView {
  path: string;
  label: string;
  result: boolean;
  children?: ExplanationView[];
}

export interface GroupExplanationView {
  account: AccountSummaryView;
  explanation: ExplanationView;
}

/** An application that currently selects this group, for the impact view. */
export interface GroupApplicationView {
  iconUrl?: string;
  kind: string;
  appId: string;
  displayName: string;
}

export interface PageGroupApplicationView {
  items: GroupApplicationView[] | null;
  nextCursor: string;
}

/**
 * `POST /groups/rule-preview` validates an unsaved draft and pages its matches.
 * `matchedCount` is the whole-draft total, not the size of this page.
 */
export interface RulePreviewRequest {
  version: number;
  condition: AppAccessCondition;
  cursor?: string;
  limit?: number;
}

export interface RulePreviewPageView {
  items: GroupPreviewView[] | null;
  matchedCount: number;
  nextCursor: string;
}

export interface RawAdminPaths {
  /**
   * Declared here rather than read from the generated schema: Huma registers
   * this operation without `pageInput`, so the schema says "no query" while the
   * handler pages by cursor like every other admin list. Taking the generated
   * type would make a paged call impossible to type.
   */
  "/api/prohibitorum/invitations": {
    get: {
      parameters: {
        query?: { cursor?: string; limit?: number };
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["PageInvitationView"];
          };
        };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": CreateInvitationRequest };
      };
      responses: {
        200: {
          content: {
            "application/json": components["schemas"]["InvitationResponse"];
          };
        };
      };
    };
  };
  /** Answers 200 with an empty object, unlike the 204s elsewhere in the group. */
  "/api/prohibitorum/invitations/revoke": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": RevokeInvitationRequest } };
      responses: {
        200: { content: { "application/json": Record<string, never> } };
      };
    };
  };
  "/api/prohibitorum/accounts/set-disabled": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": SetAccountDisabledRequest };
      };
      responses: {
        200: {
          content: { "application/json": components["schemas"]["AccountView"] };
        };
      };
    };
  };
  "/api/prohibitorum/accounts/credentials/delete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": DeleteAccountCredentialRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/tokens/revoke": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountTokenRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/{id}/sessions/revoke": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { id: number };
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountSessionRequest };
      };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/accounts/revoke-sessions": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: {
        content: { "application/json": RevokeAccountSessionsRequest };
      };
      responses: {
        200: {
          content: { "application/json": RevokeAccountSessionsResult };
        };
      };
    };
  };
  "/api/prohibitorum/groups": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        /** A bare array, not a `{ items, nextCursor }` envelope. */
        200: { content: { "application/json": AppGroupView[] } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": CreateGroupRequest } };
      responses: {
        201: { content: { "application/json": AppGroupView } };
      };
    };
  };
  "/api/prohibitorum/groups/providers": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        /** A bare array. */
        200: { content: { "application/json": ProviderDescriptorView[] } };
      };
    };
  };
  "/api/prohibitorum/groups/rule-preview": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path?: never;
        cookie?: never;
      };
      requestBody: { content: { "application/json": RulePreviewRequest } };
      responses: {
        200: { content: { "application/json": RulePreviewPageView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": AppGroupView } };
      };
    };
    put: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpdateGroupRequest } };
      responses: {
        200: { content: { "application/json": AppGroupView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/delete": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/groups/{groupId}/decisions": {
    get: {
      parameters: {
        query?: { cursor?: string; limit?: number };
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageManualDecisionView } };
      };
    };
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": UpsertDecisionRequest } };
      responses: {
        200: { content: { "application/json": ManualDecisionView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/decisions/clear": {
    post: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody: { content: { "application/json": ClearDecisionRequest } };
      responses: { 204: { content?: never } };
    };
  };
  "/api/prohibitorum/groups/{groupId}/preview": {
    get: {
      parameters: {
        query?: { limit?: number };
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageGroupPreviewView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/explain/{accountId}": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number; accountId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": GroupExplanationView } };
      };
    };
  };
  "/api/prohibitorum/groups/{groupId}/applications": {
    get: {
      parameters: {
        query?: never;
        header?: never;
        path: { groupId: number };
        cookie?: never;
      };
      requestBody?: never;
      responses: {
        200: { content: { "application/json": PageGroupApplicationView } };
      };
    };
  };
}
