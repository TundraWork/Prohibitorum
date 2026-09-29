package pat

import "testing"

func TestParseHeader(t *testing.T) {
	tok := Prefix + "abcDEF123_-"
	ok, err := ParseHeader([]string{tok})
	if err != nil || ok != tok {
		t.Fatalf("valid header: %q, %v", ok, err)
	}
	bad := map[string][]string{
		"missing":         nil,
		"empty value":     {""},
		"empty list item": {},
		"multiple":        {tok, tok},
		"wrong prefix":    {"Bearer " + tok},
		"other prefix":    {"prohibitorum_x_abc"},
		"leading space":   {" " + tok},
		"trailing space":  {tok + " "},
		"inner space":     {tok + " x"},
		"tab":             {tok + "\t"},
		"comma":           {tok + "," + tok},
	}
	for name, v := range bad {
		if _, err := ParseHeader(v); err == nil {
			t.Errorf("%s: accepted %q", name, v)
		}
	}
}
