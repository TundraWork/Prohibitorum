package federation_test

import (
	"encoding/json"
	"strings"
	"testing"

	"prohibitorum/pkg/federation"
)

func TestValidateUpstreamData(t *testing.T) {
	t.Parallel()

	valid := map[string]string{"userId": "usr_123", "displayName": "Alice"}
	got, err := federation.ValidateUpstreamData(valid)
	if err != nil {
		t.Fatalf("ValidateUpstreamData(valid): %v", err)
	}
	if string(got) != `{"displayName":"Alice","userId":"usr_123"}` {
		t.Fatalf("compact JSON = %s", got)
	}

	empty, err := federation.ValidateUpstreamData(nil)
	if err != nil {
		t.Fatalf("ValidateUpstreamData(nil): %v", err)
	}
	if string(empty) != `{}` {
		t.Fatalf("nil data = %s, want {}", empty)
	}

	tooMany := make(map[string]string, 17)
	for i := range 17 {
		tooMany[string(rune('a'+i))] = "value"
	}
	for _, tc := range []struct {
		name string
		data map[string]string
	}{
		{name: "too many keys", data: tooMany},
		{name: "key too long", data: map[string]string{strings.Repeat("k", 129): "value"}},
		{name: "value too long", data: map[string]string{"key": strings.Repeat("v", 1025)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := federation.ValidateUpstreamData(tc.data); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateUpstreamDataConservativeBudget(t *testing.T) {
	t.Parallel()

	data := map[string]string{
		"a": strings.Repeat("a", 1024),
		"b": strings.Repeat("b", 1024),
		"c": strings.Repeat("c", 1024),
		"d": strings.Repeat("d", 1024),
	}
	for {
		indented, err := json.MarshalIndent(data, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		if len(indented) <= 4096 {
			break
		}
		data["d"] = data["d"][:len(data["d"])-1]
	}
	if _, err := federation.ValidateUpstreamData(data); err != nil {
		t.Fatalf("maximum accepted fixture rejected: %v", err)
	}
	data["d"] += "d"
	if _, err := federation.ValidateUpstreamData(data); err == nil {
		t.Fatal("next byte beyond conservative budget accepted")
	}
}
