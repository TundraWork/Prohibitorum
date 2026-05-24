package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/server"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Options struct{}

func main() {
	cli := humacli.New(func(h humacli.Hooks, _ *Options) {
		h.OnStart(func() {
			ctx := context.Background()
			s, err := server.NewServer(ctx)
			if err != nil {
				log.Fatalf("create server: %v", err)
			}
			if err := s.Serve(); err != nil {
				log.Fatalf("serve: %v", err)
			}
		})
	})

	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec to stdout",
		Run: func(_ *cobra.Command, _ []string) {
			api := server.NewHuma()
			b, _ := api.OpenAPI().DowngradeYAML()
			fmt.Println(string(b))
		},
	})

	var (
		enrollNew      bool
		enrollReset    bool
		enrollUsername string
	)
	enrollCmd := &cobra.Command{
		Use:   "enroll-admin",
		Short: "Issue a passkey enrollment URL for an admin account",
		Long: `Issue a one-time enrollment URL the operator opens in a browser to register
a passkey for an admin account.

Default behavior errors if an admin already exists; pass --new to add another
admin or --reset --username NAME to recover a specific existing admin.`,
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			config, err := configx.Parse()
			if err != nil {
				log.Fatalf("parse config: %v", err)
			}
			if len(config.PublicOrigins) == 0 {
				log.Fatalf("PROHIBITORUM_PUBLIC_ORIGIN is not set; the enrollment URL needs a base origin")
			}
			if _, err := migrations.UpWithResult(config.DatabaseURL); err != nil {
				log.Fatalf("apply migrations: %v", err)
			}
			conn, err := pgxpool.New(ctx, config.DatabaseURL)
			if err != nil {
				log.Fatalf("connect db: %v", err)
			}
			defer conn.Close()
			q := db.New(conn)

			var (
				token, label string
				exp          time.Time
			)
			switch {
			case enrollReset:
				if enrollUsername == "" {
					log.Fatalf("--reset requires --username NAME")
				}
				a, err := q.GetAccountByUsername(ctx, enrollUsername)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						log.Fatalf("user %q not found", enrollUsername)
					}
					log.Fatalf("look up user: %v", err)
				}
				if a.Role != "admin" {
					log.Fatalf("user %q has role %q; --reset only handles admin accounts", a.Username, a.Role)
				}
				id := a.ID
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentReset, &id, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = fmt.Sprintf("Reset enrollment for %q", a.Username)
			case enrollNew:
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentBootstrap, nil, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = "Additional admin enrollment"
			default:
				has, err := q.HasAnyActiveAdmin(ctx)
				if err != nil {
					log.Fatalf("status check: %v", err)
				}
				if has {
					log.Fatalf("an admin already exists. Pass --new to add another admin, or --reset --username NAME to recover a specific admin.")
				}
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentBootstrap, nil, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = "Bootstrap admin enrollment"
			}

			logx.WithContext(ctx).WithFields(logrus.Fields{
				"event":  "auth.enrollment_issued",
				"source": "cli",
			}).Info("auth")

			url := config.PublicOrigins[0] + "/enroll/" + token
			fmt.Printf("%s URL (expires %s):\n%s\n", label, exp.Format(time.RFC3339), url)
		},
	}
	enrollCmd.Flags().BoolVar(&enrollNew, "new", false, "Always create a new admin enrollment.")
	enrollCmd.Flags().BoolVar(&enrollReset, "reset", false, "Issue a reset enrollment (with --username).")
	enrollCmd.Flags().StringVar(&enrollUsername, "username", "", "Target username for --reset.")
	cli.Root().AddCommand(enrollCmd)

	cli.Run()
}
