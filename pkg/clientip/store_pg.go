package clientip

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the production store backed by the instance_settings singleton row.
type PGStore struct{ pool *pgxpool.Pool }

// NewPGStore creates a PGStore backed by the given connection pool.
func NewPGStore(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Get(ctx context.Context) (Stored, error) {
	var out Stored
	row := s.pool.QueryRow(ctx,
		`SELECT client_ip_strategy, client_ip_header, client_ip_trusted_proxies
		   FROM instance_settings WHERE id = 1`)
	if err := row.Scan(&out.Strategy, &out.Header, &out.TrustedProxies); err != nil {
		return Stored{}, err
	}
	return out, nil
}

func (s *PGStore) Set(ctx context.Context, in Stored) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE instance_settings
		    SET client_ip_strategy = $1, client_ip_header = $2, client_ip_trusted_proxies = $3, updated_at = now()
		  WHERE id = 1`,
		in.Strategy, in.Header, in.TrustedProxies)
	return err
}
