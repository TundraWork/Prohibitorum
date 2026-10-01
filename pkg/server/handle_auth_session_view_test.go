package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// The profile page shows the account's own subject, so GET /me carries it in
// the same form the admin account view does.
func TestSessionViewCarriesOIDCSubject(t *testing.T) {
	subject := pgtype.UUID{
		Bytes: [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0x4d, 0xef, 0x80, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
		Valid: true,
	}
	s := &Server{}
	v := s.sessionView(&db.Account{ID: 7, Username: "alice", DisplayName: "Alice", Role: "user", OidcSubject: subject})
	if v.OIDCSubject != "12345678-9abc-4def-8001-020304050607" {
		t.Fatalf("OIDCSubject = %q", v.OIDCSubject)
	}
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"oidcSubject":"12345678-9abc-4def-8001-020304050607"`) {
		t.Errorf("body = %s", body)
	}
}
