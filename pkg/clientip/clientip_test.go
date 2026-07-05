package clientip

import (
	"net"
	"net/http"
	"testing"
)

func cidrs(t *testing.T, ss ...string) []*net.IPNet {
	t.Helper()
	var out []*net.IPNet
	for _, s := range ss {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatalf("bad test CIDR %q: %v", s, err)
		}
		out = append(out, n)
	}
	return out
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name       string
		cfg        Config
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{
			name:       "direct ignores headers",
			cfg:        Config{Strategy: Direct},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9", "CF-Connecting-IP": "8.8.8.8"},
			want:       "203.0.113.7",
		},
		{
			name:       "empty strategy behaves as direct",
			cfg:        Config{},
			remoteAddr: "203.0.113.7:5000",
			want:       "203.0.113.7",
		},
		{
			name:       "header trusted peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "198.51.100.23",
		},
		{
			name:       "header untrusted peer falls back to peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "192.0.2.9:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "192.0.2.9",
		},
		{
			name:       "header missing falls back to peer",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			want:       "203.0.113.7",
		},
		{
			name:       "header empty trusted list never trusts",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP"},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "203.0.113.7",
		},
		{
			name:       "forwarded skips trusted from right",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23, 203.0.113.9"},
			want:       "198.51.100.23",
		},
		{
			name:       "forwarded spoof attempt from trusted peer",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "9.9.9.9, 198.51.100.23"},
			want:       "198.51.100.23",
		},
		{
			name:       "forwarded untrusted peer ignores header",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "192.0.2.9:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23"},
			want:       "192.0.2.9",
		},
		{
			name:       "forwarded all trusted returns leftmost",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "203.0.113.1, 203.0.113.2"},
			want:       "203.0.113.1",
		},
		{
			name:       "forwarded skips malformed entries",
			cfg:        Config{Strategy: Forwarded, TrustedProxies: cidrs(t, "203.0.113.0/24")},
			remoteAddr: "203.0.113.7:5000",
			headers:    map[string]string{"X-Forwarded-For": "198.51.100.23, garbage, 203.0.113.9"},
			want:       "198.51.100.23",
		},
		{
			name:       "ipv6 peer trusted header",
			cfg:        Config{Strategy: Header, Header: "CF-Connecting-IP", TrustedProxies: cidrs(t, "2001:db8::/32")},
			remoteAddr: "[2001:db8::1]:5000",
			headers:    map[string]string{"CF-Connecting-IP": "198.51.100.23"},
			want:       "198.51.100.23",
		},
		{
			name:       "direct canonicalizes v4-in-v6 peer",
			cfg:        Config{Strategy: Direct},
			remoteAddr: "[::ffff:203.0.113.7]:5000",
			want:       "203.0.113.7",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &http.Request{RemoteAddr: tc.remoteAddr, Header: http.Header{}}
			for k, v := range tc.headers {
				r.Header.Set(k, v)
			}
			if got := Extract(r, tc.cfg); got != tc.want {
				t.Fatalf("Extract() = %q, want %q", got, tc.want)
			}
		})
	}
}
