//go:build smoke

package steam

import (
	"net"
	"net/url"
)

func smokeAllowPrivateEndpoints(allowPrivate bool) bool {
	if allowPrivate {
		return true
	}
	for _, raw := range []string{loginEndpoint, summaryEndpoint} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.User != nil {
			return false
		}
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return false
		}
	}
	return true
}
