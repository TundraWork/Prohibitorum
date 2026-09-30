package branding

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// appearanceJSON renders the default appearance as a generic map so each case
// can change, add or drop one field.
func appearanceJSON(t *testing.T) map[string]any {
	t.Helper()
	raw, err := json.Marshal(DefaultAppearance())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func path(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m = m[k].(map[string]any)
	}
	return m
}

func TestDecodeAppearance(t *testing.T) {
	set := func(value any, keys ...string) func(map[string]any) {
		return func(m map[string]any) {
			path(m, keys[:len(keys)-1]...)[keys[len(keys)-1]] = value
		}
	}
	drop := func(keys ...string) func(map[string]any) {
		return func(m map[string]any) {
			delete(path(m, keys[:len(keys)-1]...), keys[len(keys)-1])
		}
	}
	cases := []struct {
		name   string
		edit   func(map[string]any)
		accept bool
	}{
		{"default", func(map[string]any) {}, true},
		{"every source", set("images", "background", "source"), true},
		{"unknown source", set("video", "background", "source"), false},
		{"lowercase hex", set("#0a1b2c", "background", "color"), true},
		{"uppercase hex", set("#0A1B2C", "background", "color"), false},
		{"short hex", set("#abc", "background", "color"), false},
		{"hex without hash", set("0a1b2c", "background", "color"), false},
		{"known gradient", set("ember", "background", "gradient"), true},
		{"unknown gradient", set("rainbow", "background", "gradient"), false},
		{"known market", set("en-US", "background", "bing", "market"), true},
		{"unknown market", set("en-NZ", "background", "bing", "market"), false},
		{"lowercase market", set("zh-cn", "background", "bing", "market"), false},
		{"query", set("snowy mountains", "background", "unsplash", "query"), true},
		{"query of 64 characters", set(strings.Repeat("山", 64), "background", "unsplash", "query"), true},
		{"query of 65 characters", set(strings.Repeat("a", 65), "background", "unsplash", "query"), false},
		{"query with leading space", set(" sea", "background", "unsplash", "query"), false},
		{"query with trailing space", set("sea ", "background", "unsplash", "query"), false},
		{"query with a control character", set("sea\tside", "background", "unsplash", "query"), false},
		{"carousel", set("carousel", "background", "images", "order"), true},
		{"unknown order", set("shuffle", "background", "images", "order"), false},
		{"interval 5", set(5, "background", "images", "intervalSeconds"), true},
		{"interval 3600", set(3600, "background", "images", "intervalSeconds"), true},
		{"interval 4", set(4, "background", "images", "intervalSeconds"), false},
		{"interval 3601", set(3601, "background", "images", "intervalSeconds"), false},
		{"fractional interval", set(7.5, "background", "images", "intervalSeconds"), false},
		{"opacity 0", set(0, "card", "opacity"), true},
		{"opacity 100", set(100, "capsules", "opacity"), true},
		{"opacity -1", set(-1, "card", "opacity"), false},
		{"opacity 101", set(101, "capsules", "opacity"), false},
		{"string opacity", set("80", "card", "opacity"), false},
		{"null field", set(nil, "background", "color"), false},
		{"unknown top-level field", set(true, "extra"), false},
		{"unknown nested field", set("x", "background", "bing", "extra"), false},
		{"missing card", drop("card"), false},
		{"missing blur", drop("capsules", "blur"), false},
		{"missing images", drop("background", "images"), false},
		{"missing showCaption", drop("background", "bing", "showCaption"), false},
		{"missing query", drop("background", "unsplash", "query"), false},
		{"card on the left", set("left", "cardPosition"), true},
		{"card on the right", set("right", "cardPosition"), true},
		{"unknown card position", set("middle", "cardPosition"), false},
		{"capitalised card position", set("Left", "cardPosition"), false},
		{"empty card position", set("", "cardPosition"), false},
		{"null card position", set(nil, "cardPosition"), false},
		{"numeric card position", set(1, "cardPosition"), false},
		{"missing card position", drop("cardPosition"), false},
		{"always light", set("light", "theme"), true},
		{"always dark", set("dark", "theme"), true},
		{"system theme", set("system", "theme"), false},
		{"capitalised theme", set("Dark", "theme"), false},
		{"empty theme", set("", "theme"), false},
		{"null theme", set(nil, "theme"), false},
		{"missing theme", drop("theme"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := appearanceJSON(t)
			tc.edit(m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeAppearance(raw)
			if tc.accept && err != nil {
				t.Fatalf("DecodeAppearance(%s) = %v, want accepted", raw, err)
			}
			if !tc.accept && !errors.Is(err, ErrInvalidAppearance) {
				t.Fatalf("DecodeAppearance(%s) = %v, want ErrInvalidAppearance", raw, err)
			}
		})
	}
}

func TestDecodeAppearanceRoundTrip(t *testing.T) {
	want := DefaultAppearance()
	want.Background.Source = SourceBing
	want.Background.Bing = BingOptions{Market: "ja-JP", ShowCaption: false}
	want.Capsules = Surface{Translucent: false, Opacity: 35, Blur: false}
	want.CardPosition = CardRight
	want.Theme = ThemeDark
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeAppearance(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	if _, err := DecodeAppearance(append(raw, []byte(`{}`)...)); !errors.Is(err, ErrInvalidAppearance) {
		t.Fatalf("trailing data: err = %v, want ErrInvalidAppearance", err)
	}
}
