package contract

import "time"

// AppManagerView is one account assignment shown on an admin application's
// assignment surface. The name is retained for API compatibility.
type AppManagerView struct {
	ID          int32     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Disabled    bool      `json:"disabled"`
	AssignedAt  time.Time `json:"assignedAt"`
}

// AppSummaryView is the safe, protocol-neutral identity of one managed
// application. AppID is the route identifier: an OIDC client ID for oidc and
// forward_auth, or the decimal SAML SP ID for saml.
type AppSummaryView struct {
	IconURL           *string        `json:"iconUrl,omitempty"`
	Kind              string         `json:"kind"`
	AppID             string         `json:"appId"`
	DisplayName       string         `json:"displayName"`
	LaunchURL         string         `json:"launchUrl,omitempty"`
	RedirectURIs      []string       `json:"redirectUris,omitempty"`
	EntityID          string         `json:"entityId,omitempty"`
	ForwardAuthHost   string         `json:"forwardAuthHost,omitempty"`
	ForwardAuthScopes []AppScopeView `json:"forwardAuthScopes,omitempty"`
	AccessRestricted  bool           `json:"accessRestricted"`
}

// AppScopeView is one safe forward-auth scope advertised by an application.
type AppScopeView struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProviderDescriptorView is the safe policy-authoring identity of one known
// upstream provider. Disabled and invite-only providers remain selectable so
// existing verified connections do not become impossible to express.
type ProviderDescriptorView struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}

// AppAccessWorkspace combines the application summary with selected global groups.
type AppAccessWorkspace struct {
	App              AppSummaryView           `json:"app"`
	AccessRestricted bool                     `json:"accessRestricted"`
	Providers        []ProviderDescriptorView `json:"providers"`
	Groups           []AppGroupView           `json:"groups"`
}

// AppAccessRule is the public mirror of the closed appaccess rule document.
// Keeping this wire type in contract prevents server internals from becoming a
// public API dependency.
type AppAccessRule struct {
	Version   int                `json:"version"`
	Condition AppAccessCondition `json:"condition"`
}

// AppAccessCondition is one bounded rule AST node.
type AppAccessCondition struct {
	Op       string               `json:"op,omitempty"`
	Children []AppAccessCondition `json:"children,omitempty"`
	Child    *AppAccessCondition  `json:"child,omitempty"`
	Fact     string               `json:"fact,omitempty"`
	Provider string               `json:"provider,omitempty"`
	Protocol string               `json:"protocol,omitempty"`
	Method   string               `json:"method,omitempty"`
	Source   string               `json:"source,omitempty"`
}

// AppGroupView is one reusable global policy group.
type AppGroupView struct {
	ID                  int32          `json:"id"`
	Kind                string         `json:"kind"`
	Slug                string         `json:"slug"`
	DisplayName         string         `json:"displayName"`
	Description         string         `json:"description,omitempty"`
	ExposedToDownstream bool           `json:"exposedToDownstream"`
	Rule                *AppAccessRule `json:"rule,omitempty"`
}

// GroupApplicationView identifies an application that currently selects a
// reusable group. It contains only fields needed to assess shared impact.
type GroupApplicationView struct {
	Kind        string `json:"kind"`
	AppID       string `json:"appId"`
	DisplayName string `json:"displayName"`
}

// AccountSummaryView contains only fields safe to expose in policy management
// previews, explanations, and manual decisions.
type AccountSummaryView struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// ManualDecisionView is one per-account allow or deny in an app's manual group.
type ManualDecisionView struct {
	Account   AccountSummaryView `json:"account"`
	Effect    string             `json:"effect"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// GroupPreviewView reports whether a safe account summary matches one rule.
type GroupPreviewView struct {
	Account AccountSummaryView `json:"account"`
	Matched bool               `json:"matched"`
}

// RulePreviewPageView reports the whole-draft match count and one cursor page
// of safe account summaries for an unsaved application rule.
type RulePreviewPageView struct {
	Items        []GroupPreviewView `json:"items"`
	MatchedCount int                `json:"matchedCount"`
	NextCursor   string             `json:"nextCursor"`
}

// ExplanationView is a bounded, evaluator-safe condition result tree.
type ExplanationView struct {
	Path     string            `json:"path"`
	Label    string            `json:"label"`
	Result   bool              `json:"result"`
	Children []ExplanationView `json:"children,omitempty"`
}

// GroupExplanationView combines a safe account summary with its rule result
// tree; it intentionally does not expose the account's underlying facts.
type GroupExplanationView struct {
	Account     AccountSummaryView `json:"account"`
	Explanation ExplanationView    `json:"explanation"`
}
