package branding

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the production store backed by the instance_settings singleton row.
type PGStore struct{ pool *pgxpool.Pool }

// NewPGStore creates a PGStore backed by the given connection pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Get(ctx context.Context) (Settings, error) {
	var out Settings
	row := s.pool.QueryRow(ctx,
		`SELECT instance_name, icon_png, icon_etag, maintenance_mode, maintenance_message, login_bg, login_bg_etag
		   FROM instance_settings WHERE id = 1`)
	var name *string
	var icon []byte
	var etag *string
	var maintenance bool
	var message *string
	var loginBG []byte
	var loginBGEtag *string
	if err := row.Scan(&name, &icon, &etag, &maintenance, &message, &loginBG, &loginBGEtag); err != nil {
		return Settings{}, err
	}
	out.Name, out.IconPNG, out.IconEtag = name, icon, etag
	out.Maintenance, out.MaintenanceMessage = maintenance, message
	out.LoginBG, out.LoginBGEtag = loginBG, loginBGEtag
	return out, nil
}

func (s *PGStore) SetMaintenance(ctx context.Context, on bool, message *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET maintenance_mode = $1, maintenance_message = $2, updated_at = now() WHERE id = 1`,
		on, message)
	return err
}

func (s *PGStore) SetName(ctx context.Context, name *string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET instance_name = $1, updated_at = now() WHERE id = 1`, name)
	return err
}

func (s *PGStore) SetIcon(ctx context.Context, png []byte, etag string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET icon_png = $1, icon_etag = $2, updated_at = now() WHERE id = 1`, png, etag)
	return err
}

func (s *PGStore) ClearIcon(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET icon_png = NULL, icon_etag = NULL, updated_at = now() WHERE id = 1`)
	return err
}

func (s *PGStore) SetLoginBG(ctx context.Context, raw []byte, etag string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET login_bg = $1, login_bg_etag = $2, updated_at = now() WHERE id = 1`, raw, etag)
	return err
}

func (s *PGStore) ClearLoginBG(ctx context.Context) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings SET login_bg = NULL, login_bg_etag = NULL, updated_at = now() WHERE id = 1`)
	return err
}
