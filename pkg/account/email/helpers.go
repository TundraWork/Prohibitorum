package email

import (
	"github.com/jackc/pgx/v5/pgtype"
)

func pgInt8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

func pgInt8Ptr(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *v, Valid: true}
}

func pgText(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: true}
}
