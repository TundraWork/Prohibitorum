package authn

// Permits and PermissionsView were removed in the v0.1 schema rewrite.
// Permission gates are now implemented via account.Attributes (map[string]any).
// Role-based gates (admin vs user) are enforced by middleware and handlers.
