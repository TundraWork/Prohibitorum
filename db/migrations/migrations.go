package migrations

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"

	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed *.sql
var embedMigrations embed.FS

type MigrationResult struct {
	PreviousVersion int64
	CurrentVersion  int64
}

func Up(connURL string) error {
	_, err := UpWithResult(connURL)
	return err
}

func UpWithResult(connURL string) (MigrationResult, error) {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return MigrationResult{}, err
	}

	db, err := sql.Open("pgx", connURL)
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
