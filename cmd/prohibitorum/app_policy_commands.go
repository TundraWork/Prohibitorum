package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/spf13/cobra"

	"prohibitorum/pkg/appaccess"
	"prohibitorum/pkg/db"
)

const maxRuleFileBytes = 64 << 10

type appPolicyCLI struct {
	kind       appaccess.AppKind
	flag       string
	targetName string
}

func addAppPolicyCommands(parent *cobra.Command, cfg appPolicyCLI) {
	addAppManagerCommands(parent, cfg)
	addAppRestrictionCommands(parent, cfg)
	addAppGroupCommands(parent, cfg)
	addAppDecisionCommands(parent, cfg)
}

func (c appPolicyCLI) bindTargetFlag(cmd *cobra.Command, dst *string) {
	cmd.PersistentFlags().StringVar(dst, c.flag, "", c.targetName+" (required).")
}

func (c appPolicyCLI) resolve(ctx context.Context, q *db.Queries, identifier string) (appaccess.AppRef, error) {
	if identifier == "" {
		return appaccess.AppRef{}, fmt.Errorf("--%s is required", c.flag)
	}
	switch c.kind {
	case appaccess.KindOIDC, appaccess.KindForwardAuth:
		client, err := q.GetOIDCClientAny(ctx, identifier)
		if err != nil {
			return appaccess.AppRef{}, err
		}
		if (c.kind == appaccess.KindOIDC && client.ForwardAuthEnabled) || (c.kind == appaccess.KindForwardAuth && !client.ForwardAuthEnabled) {
			return appaccess.AppRef{}, pgx.ErrNoRows
		}
		return appaccess.AppRef{Kind: c.kind, OIDCClientID: client.ClientID}, nil
	case appaccess.KindSAML:
		sp, err := q.GetSAMLSPByEntityID(ctx, identifier)
		if err != nil {
			return appaccess.AppRef{}, err
		}
		return appaccess.AppRef{Kind: appaccess.KindSAML, SAMLSPID: sp.ID}, nil
	default:
		return appaccess.AppRef{}, fmt.Errorf("unsupported application kind %q", c.kind)
	}
}

func (c appPolicyCLI) mustResolve(ctx context.Context, q *db.Queries, identifier, operation string) appaccess.AppRef {
	ref, err := c.resolve(ctx, q, identifier)
	if errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("%s: application %q not found", operation, identifier)
	}
	if err != nil {
		log.Fatalf("%s: resolve application: %v", operation, err)
	}
	return ref
}

func listPolicyGroups(ctx context.Context, q *db.Queries, ref appaccess.AppRef) ([]db.UserGroup, error) {
	if ref.Kind == appaccess.KindSAML {
		return q.ListSAMLAppGroups(ctx, ref.SAMLSPID)
	}
	return q.ListOIDCAppGroups(ctx, ref.OIDCClientID)
}

func findPolicyGroup(ctx context.Context, q *db.Queries, ref appaccess.AppRef, slug string) (db.UserGroup, error) {
	groups, err := listPolicyGroups(ctx, q, ref)
	if err != nil {
		return db.UserGroup{}, err
	}
	for _, group := range groups {
		if group.Slug == slug {
			return group, nil
		}
	}
	return db.UserGroup{}, pgx.ErrNoRows
}

func findManualPolicyGroup(ctx context.Context, q *db.Queries, ref appaccess.AppRef) (db.UserGroup, error) {
	groups, err := listPolicyGroups(ctx, q, ref)
	if err != nil {
		return db.UserGroup{}, err
	}
	for _, group := range groups {
		if group.Kind == "manual" {
			return group, nil
		}
	}
	return db.UserGroup{}, pgx.ErrNoRows
}

func addAppManagerCommands(parent *cobra.Command, cfg appPolicyCLI) {
	var target string
	managerCmd := &cobra.Command{Use: "manager", Short: "Manage accounts assigned to applications"}
	cfg.bindTargetFlag(managerCmd, &target)

	managerCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List accounts assigned to an application",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "manager list")
			fmt.Printf("%-32s %-32s %-12s %s\n", "USERNAME", "DISPLAY_NAME", "ROLE", "DISABLED")
			if ref.Kind == appaccess.KindSAML {
				rows, err := q.ListSAMLSPManagers(ctx, ref.SAMLSPID)
				if err != nil {
					log.Fatalf("manager list: %v", err)
				}
				for _, row := range rows {
					fmt.Printf("%-32s %-32s %-12s %t\n", row.Username, row.DisplayName, row.Role, row.Disabled)
				}
				return
			}
			rows, err := q.ListOIDCClientManagers(ctx, ref.OIDCClientID)
			if err != nil {
				log.Fatalf("manager list: %v", err)
			}
			for _, row := range rows {
				fmt.Printf("%-32s %-32s %-12s %t\n", row.Username, row.DisplayName, row.Role, row.Disabled)
			}
		},
	})

	var assignUsername string
	assignCmd := &cobra.Command{
		Use:   "assign",
		Short: "Assign an account to an application",
		Run: func(_ *cobra.Command, _ []string) {
			if assignUsername == "" {
				log.Fatalf("--username is required")
			}
			ctx := context.Background()
			_, conn := mustOpenDB(ctx)
			defer conn.Close()
			tx, err := conn.Begin(ctx)
			if err != nil {
				log.Fatalf("manager assign: begin transaction: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			q := db.New(tx)
			ref := cfg.mustResolve(ctx, q, target, "manager assign")
			account, err := q.GetAccountByUsername(ctx, assignUsername)
			if errors.Is(err, pgx.ErrNoRows) {
				log.Fatalf("manager assign: account %q not found", assignUsername)
			}
			if err != nil {
				log.Fatalf("manager assign: account lookup: %v", err)
			}
			account, err = q.GetAccountByIDForUpdate(ctx, account.ID)
			if err != nil {
				log.Fatalf("manager assign: lock account: %v", err)
			}
			if account.Disabled {
				log.Fatalf("manager assign: account %q must be enabled", assignUsername)
			}
			if ref.Kind == appaccess.KindSAML {
				err = q.AssignSAMLSPManager(ctx, db.AssignSAMLSPManagerParams{SamlSpID: ref.SAMLSPID, AccountID: account.ID})
			} else {
				err = q.AssignOIDCClientManager(ctx, db.AssignOIDCClientManagerParams{ClientID: ref.OIDCClientID, AccountID: account.ID})
			}
			if err != nil {
				log.Fatalf("manager assign: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				log.Fatalf("manager assign: commit: %v", err)
			}
			fmt.Printf("Assigned %q as manager\n", assignUsername)
		},
	}
	assignCmd.Flags().StringVar(&assignUsername, "username", "", "Account username (required).")
	managerCmd.AddCommand(assignCmd)

	var removeUsername string
	removeCmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove an account assignment from an application",
		Run: func(_ *cobra.Command, _ []string) {
			if removeUsername == "" {
				log.Fatalf("--username is required")
			}
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "manager remove")
			account, err := q.GetAccountByUsername(ctx, removeUsername)
			if errors.Is(err, pgx.ErrNoRows) {
				log.Fatalf("manager remove: account %q not found", removeUsername)
			}
			if err != nil {
				log.Fatalf("manager remove: account lookup: %v", err)
			}
			var rows int64
			if ref.Kind == appaccess.KindSAML {
				rows, err = q.RemoveSAMLSPManager(ctx, db.RemoveSAMLSPManagerParams{SamlSpID: ref.SAMLSPID, AccountID: account.ID})
			} else {
				rows, err = q.RemoveOIDCClientManager(ctx, db.RemoveOIDCClientManagerParams{ClientID: ref.OIDCClientID, AccountID: account.ID})
			}
			if err != nil {
				log.Fatalf("manager remove: %v", err)
			}
			if rows == 0 {
				log.Fatalf("manager remove: %q is not assigned", removeUsername)
			}
			fmt.Printf("Removed manager %q\n", removeUsername)
		},
	}
	removeCmd.Flags().StringVar(&removeUsername, "username", "", "assigned manager username (required).")
	managerCmd.AddCommand(removeCmd)
	parent.AddCommand(managerCmd)
}

func addAppRestrictionCommands(parent *cobra.Command, cfg appPolicyCLI) {
	var target string
	accessCmd := &cobra.Command{Use: "access", Short: "Manage the application access gate"}
	cfg.bindTargetFlag(accessCmd, &target)
	var restricted bool
	setCmd := &cobra.Command{
		Use:   "set-restricted",
		Short: "Enable or disable restricted access",
		Run: func(cmd *cobra.Command, _ []string) {
			if !cmd.Flags().Changed("restricted") {
				log.Fatalf("--restricted=true or --restricted=false is required")
			}
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "access set-restricted")
			var err error
			if ref.Kind == appaccess.KindSAML {
				_, err = q.SetSAMLSPAccessRestricted(ctx, db.SetSAMLSPAccessRestrictedParams{SamlSpID: ref.SAMLSPID, AccessRestricted: restricted})
			} else {
				_, err = q.SetOIDCClientAccessRestricted(ctx, db.SetOIDCClientAccessRestrictedParams{ClientID: ref.OIDCClientID, AccessRestricted: restricted})
			}
			if err != nil {
				log.Fatalf("access set-restricted: %v", err)
			}
			fmt.Printf("Application access_restricted=%t\n", restricted)
		},
	}
	setCmd.Flags().BoolVar(&restricted, "restricted", false, "Restricted access state.")
	accessCmd.AddCommand(setCmd)
	parent.AddCommand(accessCmd)
}

func replacePolicyGroups(ctx context.Context, q *db.Queries, ref appaccess.AppRef, groupIDs []int32) ([]db.UserGroup, error) {
	if ref.Kind == appaccess.KindSAML {
		return q.ReplaceSAMLAppGroups(ctx, db.ReplaceSAMLAppGroupsParams{SamlSpID: ref.SAMLSPID, GroupIds: groupIDs})
	}
	return q.ReplaceOIDCAppGroups(ctx, db.ReplaceOIDCAppGroupsParams{OidcClientID: ref.OIDCClientID, GroupIds: groupIDs})
}

func findBoundPolicyGroup(ctx context.Context, q *db.Queries, ref appaccess.AppRef, groupID int32) (db.UserGroup, error) {
	if ref.Kind == appaccess.KindSAML {
		return q.GetSAMLAppGroup(ctx, db.GetSAMLAppGroupParams{GroupID: groupID, SamlSpID: ref.SAMLSPID})
	}
	return q.GetOIDCAppGroup(ctx, db.GetOIDCAppGroupParams{GroupID: groupID, OidcClientID: ref.OIDCClientID})
}

func addAppGroupCommands(parent *cobra.Command, cfg appPolicyCLI) {
	var target string
	groupCmd := &cobra.Command{Use: "group", Short: "Select global groups for the application"}
	cfg.bindTargetFlag(groupCmd, &target)

	groupCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List global groups selected by the application",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "group list")
			groups, err := listPolicyGroups(ctx, q, ref)
			if err != nil {
				log.Fatalf("group list: %v", err)
			}
			fmt.Printf("%-8s %-12s %-32s %-32s %s\n", "ID", "KIND", "SLUG", "DISPLAY_NAME", "EXPOSED")
			for _, group := range groups {
				fmt.Printf("%-8d %-12s %-32s %-32s %t\n", group.ID, group.Kind, group.Slug, group.DisplayName, group.ExposedToDownstream)
			}
		},
	})

	var groupIDs []int32
	selectCmd := &cobra.Command{
		Use:   "select",
		Short: "Replace the complete global-group selection",
		Run: func(_ *cobra.Command, _ []string) {
			seen := make(map[int32]struct{}, len(groupIDs))
			for _, id := range groupIDs {
				if id <= 0 {
					log.Fatalf("group select: every --group-id must be positive")
				}
				if _, duplicate := seen[id]; duplicate {
					log.Fatalf("group select: duplicate group id %d", id)
				}
				seen[id] = struct{}{}
			}
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "group select")
			groups, err := replacePolicyGroups(ctx, q, ref, groupIDs)
			if err != nil {
				log.Fatalf("group select: %v", err)
			}
			if len(groups) != len(groupIDs) {
				log.Fatalf("group select: one or more global groups do not exist")
			}
			fmt.Printf("Selected %d global group(s)\n", len(groups))
		},
	}
	selectCmd.Flags().Int32SliceVar(&groupIDs, "group-id", nil, "Global group ID to select; repeat for multiple groups. Omit to clear all groups.")
	groupCmd.AddCommand(selectCmd)

	var previewGroupID int32
	var limit int32
	previewCmd := &cobra.Command{
		Use:   "preview",
		Short: "Preview one selected rule group against active accounts",
		Run: func(_ *cobra.Command, _ []string) {
			if previewGroupID <= 0 {
				log.Fatalf("--group-id must be positive")
			}
			if limit < 1 || limit > 500 {
				log.Fatalf("--limit must be between 1 and 500")
			}
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "group preview")
			group, err := findBoundPolicyGroup(ctx, q, ref, previewGroupID)
			if errors.Is(err, pgx.ErrNoRows) {
				log.Fatalf("group preview: selected group %d not found", previewGroupID)
			}
			if err != nil {
				log.Fatalf("group preview: lookup: %v", err)
			}
			if group.Kind != "rule" {
				log.Fatalf("group preview: group %d is not rule-based", previewGroupID)
			}
			previews, err := appaccess.NewService(q).PreviewGroup(ctx, ref, group.ID, db.ListActiveAccountAccessFactsPageParams{RowLimit: limit})
			if err != nil {
				log.Fatalf("group preview: %v", err)
			}
			fmt.Printf("%-32s %-32s %s\n", "USERNAME", "DISPLAY_NAME", "MATCHED")
			for _, preview := range previews {
				fmt.Printf("%-32s %-32s %t\n", preview.Account.Username, preview.Account.DisplayName, preview.Matched)
			}
		},
	}
	previewCmd.Flags().Int32Var(&previewGroupID, "group-id", 0, "Selected rule group ID (required).")
	previewCmd.Flags().Int32Var(&limit, "limit", 100, "Maximum active accounts to preview (1-500).")
	groupCmd.AddCommand(previewCmd)
	parent.AddCommand(groupCmd)
}

func addAppDecisionCommands(parent *cobra.Command, cfg appPolicyCLI) {
	var target string
	var groupID int32
	decisionCmd := &cobra.Command{Use: "decision", Short: "Manage decisions on a selected global manual group"}
	cfg.bindTargetFlag(decisionCmd, &target)
	decisionCmd.PersistentFlags().Int32Var(&groupID, "group-id", 0, "Selected global manual group ID (required).")

	resolveManual := func(ctx context.Context, q *db.Queries, ref appaccess.AppRef, operation string) db.UserGroup {
		if groupID <= 0 {
			log.Fatalf("%s: --group-id must be positive", operation)
		}
		group, err := findBoundPolicyGroup(ctx, q, ref, groupID)
		if errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("%s: selected group %d not found", operation, groupID)
		}
		if err != nil {
			log.Fatalf("%s: group lookup: %v", operation, err)
		}
		if group.Kind != "manual" {
			log.Fatalf("%s: group %d is not manual", operation, groupID)
		}
		return group
	}

	decisionCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List decisions for the selected manual group",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "decision list")
			group := resolveManual(ctx, q, ref, "decision list")
			rows, err := q.ListManualDecisionsPage(ctx, db.ListManualDecisionsPageParams{GroupID: group.ID, RowLimit: 10000})
			if err != nil {
				log.Fatalf("decision list: %v", err)
			}
			fmt.Printf("%-32s %-32s %s\n", "USERNAME", "DISPLAY_NAME", "EFFECT")
			for _, row := range rows {
				fmt.Printf("%-32s %-32s %s\n", row.Username, row.DisplayName, row.Effect)
			}
		},
	})

	var username, effect string
	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set or clear a decision on the selected manual group",
		Run: func(_ *cobra.Command, _ []string) {
			if username == "" {
				log.Fatalf("--username is required")
			}
			if effect != "allow" && effect != "deny" && effect != "clear" {
				log.Fatalf("--effect must be allow, deny, or clear")
			}
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			ref := cfg.mustResolve(ctx, q, target, "decision set")
			group := resolveManual(ctx, q, ref, "decision set")
			account, err := q.GetAccountByUsername(ctx, username)
			if errors.Is(err, pgx.ErrNoRows) {
				log.Fatalf("decision set: account %q not found", username)
			}
			if err != nil {
				log.Fatalf("decision set: account lookup: %v", err)
			}
			if effect == "clear" {
				if _, err := q.ClearManualDecision(ctx, db.ClearManualDecisionParams{GroupID: group.ID, AccountID: account.ID}); err != nil {
					log.Fatalf("decision set: clear: %v", err)
				}
				fmt.Printf("Cleared manual decision for %q\n", username)
				return
			}
			if _, err := q.UpsertManualDecision(ctx, db.UpsertManualDecisionParams{GroupID: group.ID, AccountID: account.ID, Effect: effect}); err != nil {
				log.Fatalf("decision set: %v", err)
			}
			fmt.Printf("Set %q decision to %s\n", username, effect)
		},
	}
	setCmd.Flags().StringVar(&username, "username", "", "Account username (required).")
	setCmd.Flags().StringVar(&effect, "effect", "", "Decision: allow, deny, or clear (required).")
	decisionCmd.AddCommand(setCmd)
	parent.AddCommand(decisionCmd)
}

func nullableText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func canonicalRuleFile(ctx context.Context, q *db.Queries, path string) ([]byte, error) {
	slugs, err := q.ListKnownUpstreamIDPSlugs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list known providers: %w", err)
	}
	providers := make(map[string]struct{}, len(slugs))
	for _, slug := range slugs {
		providers[slug] = struct{}{}
	}
	return readCanonicalRuleFile(path, providers)
}

func readCanonicalRuleFile(path string, providers map[string]struct{}) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open rule file: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxRuleFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read rule file: %w", err)
	}
	if len(raw) > maxRuleFileBytes {
		return nil, fmt.Errorf("rule file exceeds %d bytes", maxRuleFileBytes)
	}
	rule, err := appaccess.ParseAndValidateRule(raw, providers)
	if err != nil {
		return nil, fmt.Errorf("validate rule: %w", err)
	}
	canonical, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("encode canonical rule: %w", err)
	}
	return canonical, nil
}
