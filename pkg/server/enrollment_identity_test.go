package server

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
)

// recheckInviteIdentityProvider is the last line of defence before an invite
// completion writes its verified identity: whatever the operator did to the
// provider between verification and completion, the write only happens for a
// provider that could still accept it. Each case pins one leg of that check
// against the production error, so a dropped leg fails by name.
func TestRecheckInviteIdentityProvider(t *testing.T) {
	base := validVRChatEnrollmentProvider()

	cases := map[string]struct {
		row      func(db.Enrollment) db.Enrollment //nolint:revive // constructor shorthand
		provider func(db.UpstreamIdp) db.UpstreamIdp
		wantErr  bool
	}{
		"healthy provider passes": {
			wantErr: false,
		},
		"missing snapshot id": {
			row: func(e db.Enrollment) db.Enrollment {
				e.FederatedUpstreamIdpID = pgtype.Int8{}
				return e
			},
			wantErr: true,
		},
		"provider deleted mid-flight": {
			wantErr: true,
		},
		"provider disabled mid-flight": {
			provider: func(p db.UpstreamIdp) db.UpstreamIdp {
				p.Disabled = true
				return p
			},
			wantErr: true,
		},
		"provider switched to link_only": {
			provider: func(p db.UpstreamIdp) db.UpstreamIdp {
				p.Mode = federation.ModeLinkOnly
				return p
			},
			wantErr: true,
		},
		"provider slug renamed mid-flight": {
			provider: func(p db.UpstreamIdp) db.UpstreamIdp {
				p.Slug = "renamed"
				return p
			},
			wantErr: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			q := &vrchatEnrollmentQueries{
				providers: map[int64]db.UpstreamIdp{base.ID: base},
			}
			if name == "provider deleted mid-flight" {
				delete(q.providers, base.ID)
			} else if tc.provider != nil {
				q.providers[base.ID] = tc.provider(base)
			}
			// A fresh row per subtest: one case mutates the row, and a
			// shared variable would leak that mutation into whichever
			// subtest the map iteration order runs after it.
			row := verifiedInviteEnrollment("recheck")
			if tc.row != nil {
				row = tc.row(row)
			}
			provider, err := recheckInviteIdentityProvider(context.Background(), q, row)
			if tc.wantErr {
				ae := authn.AsAuthError(err)
				if ae == nil || ae.Code != "provider_not_ready" {
					t.Fatalf("error = %v, want provider_not_ready", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("healthy recheck failed: %v", err)
			}
			if provider.Slug != base.Slug || provider.ID != base.ID {
				t.Fatalf("provider = %+v, want %+v", provider, base)
			}
		})
	}
}
