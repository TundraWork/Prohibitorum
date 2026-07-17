//go:build !smoke

package steam

func smokeAllowPrivateEndpoints(allowPrivate bool) bool { return allowPrivate }
