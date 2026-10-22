package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"prohibitorum/pkg/logx"
)

//go:embed *.sql
var embedMigrations embed.FS

type MigrationResult struct {
	PreviousVersion int64
	CurrentVersion  int64
}

// Up runs all embedded migrations.
func Up(connURL string) error {
	_, err := UpWithResult(connURL)
	return err
}

// onNotice logs one Postgres NOTICE whole. The 050 migration's demotion
// notices carry the account id and address an operator must review, and
// neither the pgx DSN path nor the server writes them anywhere by default.
func onNotice(_ *pgconn.PgConn, notice *pgconn.Notice) {
	logx.New().WithFields(map[string]any{
		"migration_notice": true,
		"severity":         notice.Severity,
		"code":             notice.Code,
	}).Info(notice.Message)
}

func openNoticeDB(connURL string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(connURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}
	cfg.OnNotice = onNotice
	return stdlib.OpenDB(*cfg), nil
}

func UpWithResult(connURL string) (MigrationResult, error) {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return MigrationResult{}, err
	}

	db, err := openNoticeDB(connURL)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	previousVersion, err := goose.GetDBVersion(db)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("failed to get database version: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return MigrationResult{}, err
	}

	currentVersion, err := goose.GetDBVersion(db)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("failed to get database version: %w", err)
	}

	return MigrationResult{
		PreviousVersion: previousVersion,
		CurrentVersion:  currentVersion,
	}, nil
}
