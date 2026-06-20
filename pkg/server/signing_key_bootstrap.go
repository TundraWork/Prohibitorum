// Package server — signing_key_bootstrap.go
//
// First-boot OIDC signing-key auto-provisioning. A fresh instance has no active
// signing key, making the OIDC OP (and forward-auth) non-functional until an
// admin runs `signing-key generate`. ensureActiveSigningKey closes that gap at
// boot, reusing the same lifecycle calls as the CLI/dev tooling.
package server

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	oidcop "prohibitorum/pkg/protocol/oidc"
)

func ensureActiveSigningKey(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, cfg *configx.Config) {
	if _, err := q.GetActiveSigningKey(ctx); err == nil {
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		logx.WithContext(ctx).WithError(err).Warn("signing key: could not check for an active key; skipping auto-provision")
		return
	}

	keyVer, dek, ok := currentDEK(cfg)
	if !ok {
		logx.WithContext(ctx).Warn("signing key: no data encryption key configured; cannot auto-provision (set PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>, then run `signing-key generate`)")
		return
	}

	pending, err := oidcop.InsertPendingKey(ctx, q, dek, keyVer)
	if err != nil {
		logx.WithContext(ctx).WithError(err).Warn("signing key: auto-provision insert failed; run `signing-key generate` manually")
		return
	}
	if _, err := oidcop.ActivateSigningKey(ctx, pool, q, pending.Kid, cfg.SAML.MetadataRotationGrace); err != nil {
		logx.WithContext(ctx).WithError(err).Warn("signing key: auto-provision activate failed; run `signing-key generate` manually")
		return
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{"kid": pending.Kid}).Info("auto-provisioned initial OIDC signing key")
}

func currentDEK(cfg *configx.Config) (int32, []byte, bool) {
	if len(cfg.DataEncryptionKeys) == 0 {
		return 0, nil, false
	}
	maxVer := 0
	for v := range cfg.DataEncryptionKeys {
		if v > maxVer {
			maxVer = v
		}
	}
	return int32(maxVer), cfg.DataEncryptionKeys[maxVer], true
}
