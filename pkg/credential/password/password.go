package password

import (
	"context"
	"errors"

	"prohibitorum/pkg/db"
)

var (
	ErrPasswordNotSet    = errors.New("password: not set")
	ErrPasswordIncorrect = errors.New("password: incorrect")
)

type Store struct {
	q db.Querier
}

func NewStore(q db.Querier) *Store {
	return &Store{q: q}
}

// TODO(v0.2): fetch hash via q.GetPasswordCredential, verify via argon2id
// PHC-string verify, re-hash if params have been upgraded.
func (s *Store) Verify(ctx context.Context, accountID int32, password string) error {
	return ErrPasswordNotSet
}

// TODO(v0.2): hash via argon2id PHC, q.UpsertPasswordCredential.
func (s *Store) Set(ctx context.Context, accountID int32, password string) error {
	return errors.New("password.Set: TODO(v0.2)")
}
