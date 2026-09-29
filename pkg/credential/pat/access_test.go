package pat

import "testing"

func TestParseAccess(t *testing.T) {
	for _, a := range []Access{AccessSelectedApps, AccessAllApps, AccessFull, AccessSudo} {
		got, err := ParseAccess(string(a))
		if err != nil || got != a {
			t.Errorf("ParseAccess(%q) = %q, %v", a, got, err)
		}
	}
	for _, s := range []string{"", "Full", "SUDO", " full", "full ", "admin", "all-apps"} {
		if _, err := ParseAccess(s); err == nil {
			t.Errorf("ParseAccess(%q) accepted", s)
		}
	}
}

func TestAccessPredicates(t *testing.T) {
	cases := []struct {
		a                    Access
		api, bypass, allApps bool
	}{
		{AccessSelectedApps, false, false, false},
		{AccessAllApps, false, false, true},
		{AccessFull, true, false, true},
		{AccessSudo, true, true, true},
	}
	for _, c := range cases {
		if c.a.AllowsAPI() != c.api || c.a.BypassesSudo() != c.bypass || c.a.AllowsAllApps() != c.allApps {
			t.Errorf("%s: api=%v bypass=%v allApps=%v", c.a, c.a.AllowsAPI(), c.a.BypassesSudo(), c.a.AllowsAllApps())
		}
	}
}
