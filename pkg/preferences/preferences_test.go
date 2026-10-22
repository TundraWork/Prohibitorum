package preferences

import (
	"encoding/json"
	"errors"
	"testing"
)

func ptr(s string) *string { return &s }

func TestPatchValidate(t *testing.T) {
	cases := []struct {
		name   string
		patch  PreferencesPatch
		accept bool
	}{
		{"nothing", PreferencesPatch{}, true},
		{"unset fields", PreferencesPatch{Appearance: &AppearancePreferences{}}, true},
		{"scheme and mode", PreferencesPatch{Appearance: &AppearancePreferences{Scheme: ptr("nord"), Mode: ptr("dark")}}, true},
		{"system mode", PreferencesPatch{Appearance: &AppearancePreferences{Mode: ptr("system")}}, true},
		{"unknown scheme", PreferencesPatch{Appearance: &AppearancePreferences{Scheme: ptr("solarized")}}, false},
		{"capitalised scheme", PreferencesPatch{Appearance: &AppearancePreferences{Scheme: ptr("Nord")}}, false},
		{"empty scheme", PreferencesPatch{Appearance: &AppearancePreferences{Scheme: ptr("")}}, false},
		{"unknown mode", PreferencesPatch{Appearance: &AppearancePreferences{Mode: ptr("dim")}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.patch.Validate()
			if tc.accept && err != nil {
				t.Fatalf("Validate() = %v, want accepted", err)
			}
			if !tc.accept && !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestSectionsCarryOnlyPresentSections(t *testing.T) {
	raw, err := PreferencesPatch{}.Sections()
	if err != nil || string(raw) != "{}" {
		t.Fatalf("empty patch = %s, %v; want {}", raw, err)
	}
	raw, err = PreferencesPatch{Appearance: &AppearancePreferences{Scheme: ptr("sand")}}.Sections()
	if err != nil || string(raw) != `{"appearance":{"scheme":"sand","mode":null}}` {
		t.Fatalf("appearance patch = %s, %v", raw, err)
	}
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"empty", `{}`, `{"appearance":{"scheme":null,"mode":null}}`},
		{"missing column value", ``, `{"appearance":{"scheme":null,"mode":null}}`},
		{"set", `{"appearance":{"scheme":"citrus","mode":"light"}}`, `{"appearance":{"scheme":"citrus","mode":"light"}}`},
		{"retired scheme", `{"appearance":{"scheme":"moss","mode":"dark"}}`, `{"appearance":{"scheme":null,"mode":"dark"}}`},
		{"a later release's section", `{"appearance":{"mode":"dark"},"editor":{"tabs":2}}`, `{"appearance":{"scheme":null,"mode":"dark"}}`},
		{"corrupt", `[1]`, `{"appearance":{"scheme":null,"mode":null}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(Decode([]byte(tc.raw)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("Decode(%s) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}
}
