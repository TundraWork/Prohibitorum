package branding

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// PGStore is the production store backed by the instance_settings singleton row
// and the login_background_image table.
type PGStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewPGStore creates a PGStore backed by the given connection pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool, q: db.New(pool)} }

func (s *PGStore) Get(ctx context.Context) (Settings, error) {
	var out Settings
	row := s.pool.QueryRow(ctx,
		`SELECT instance_name, icon_png, icon_etag, maintenance_mode, maintenance_message
		   FROM instance_settings WHERE id = 1`)
	var name *string
	var icon []byte
	var etag *string
	var maintenance bool
	var message *string
	if err := row.Scan(&name, &icon, &etag, &maintenance, &message); err != nil {
		return Settings{}, err
	}
	out.Name, out.IconPNG, out.IconEtag = name, icon, etag
	out.Maintenance, out.MaintenanceMessage = maintenance, message

	look, err := s.q.GetLoginAppearance(ctx)
	if err != nil {
		return Settings{}, err
	}
	if look.LoginAppearance != nil {
		a, derr := DecodeAppearance(look.LoginAppearance)
		if derr != nil {
			// A row this build cannot read falls back to the default look
			// rather than taking the instance name and icon down with it.
			logx.WithContext(ctx).WithError(derr).Warn("branding: stored sign-in appearance unreadable; using the default")
		} else {
			out.Appearance = &a
		}
	}
	if look.UnsplashKeyVersion.Valid {
		out.UnsplashKey = &SealedKey{
			Ciphertext: look.UnsplashKeyEnc,
			Nonce:      look.UnsplashKeyNonce,
			KeyVersion: look.UnsplashKeyVersion.Int32,
		}
	}
	images, err := s.q.ListLoginBackgroundImages(ctx)
	if err != nil {
		return Settings{}, err
	}
	for _, img := range images {
		out.Images = append(out.Images, ImageRef{ID: img.ID, Etag: img.Etag})
	}
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

func (s *PGStore) SetAppearance(ctx context.Context, a Appearance, key *SealedKey) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockInstanceSettings(ctx); err != nil {
			return err
		}
		if key != nil {
			if err := q.SetUnsplashKey(ctx, db.SetUnsplashKeyParams{
				UnsplashKeyEnc:     key.Ciphertext,
				UnsplashKeyNonce:   key.Nonce,
				UnsplashKeyVersion: pgtype.Int4{Int32: key.KeyVersion, Valid: true},
			}); err != nil {
				return err
			}
		} else if a.Background.Source == SourceUnsplash {
			row, err := q.GetLoginAppearance(ctx)
			if err != nil {
				return err
			}
			if !row.UnsplashKeyVersion.Valid {
				return ErrUnsplashKeyRequired
			}
		}
		return q.SetLoginAppearance(ctx, raw)
	})
}

func (s *PGStore) ClearUnsplashKey(ctx context.Context) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockInstanceSettings(ctx); err != nil {
			return err
		}
		row, err := q.GetLoginAppearance(ctx)
		if err != nil {
			return err
		}
		if row.LoginAppearance != nil {
			if a, err := DecodeAppearance(row.LoginAppearance); err == nil && a.Background.Source == SourceUnsplash {
				return ErrUnsplashKeyInUse
			}
		}
		return q.ClearUnsplashKey(ctx)
	})
}

// AddImage locks the settings row before counting, so two concurrent uploads
// cannot both take the last slot.
func (s *PGStore) AddImage(ctx context.Context, data []byte, etag string) (ImageRef, error) {
	var ref ImageRef
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if _, err := q.LockInstanceSettings(ctx); err != nil {
			return err
		}
		n, err := q.CountLoginBackgroundImages(ctx)
		if err != nil {
			return err
		}
		if n >= MaxLoginImages {
			return ErrImageLimit
		}
		id, err := q.InsertLoginBackgroundImage(ctx, db.InsertLoginBackgroundImageParams{Data: data, Etag: etag})
		if err != nil {
			return err
		}
		ref = ImageRef{ID: id, Etag: etag}
		return nil
	})
	return ref, err
}

func (s *PGStore) DeleteImage(ctx context.Context, id int64) error {
	n, err := s.q.DeleteLoginBackgroundImage(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrImageNotFound
	}
	return nil
}

func (s *PGStore) ImageData(ctx context.Context, id int64) ([]byte, string, error) {
	row, err := s.q.GetLoginBackgroundImage(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrImageNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return row.Data, row.Etag, nil
}
