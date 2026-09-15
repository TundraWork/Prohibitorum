package server

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

func TestAccountViewsPreserveStoredOIDCSubject(t *testing.T) {
	const subject = "906b9b10-83fd-4e9f-96b2-9a648df6b233"
	var uuid pgtype.UUID
	if err := uuid.Scan(subject); err != nil {
		t.Fatal(err)
	}
	for name, view := range map[string]contract.AccountView{
		"list":    accountViewFromRow(&db.ListAccountsRow{ID: 7, Username: "carol", OidcSubject: uuid}, ""),
		"detail":  accountViewFromAccount(&db.Account{ID: 7, Username: "carol", OidcSubject: uuid}, nil, ""),
		"renamed": accountViewFromAccount(&db.Account{ID: 7, Username: "updated", DisplayName: "New Name", OidcSubject: uuid}, nil, ""),
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			if wire["oidcSubject"] != subject || wire["id"] != float64(7) {
				t.Fatalf("subject and numeric ID must remain distinct: %s", encoded)
			}
		})
	}
}
