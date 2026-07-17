//go:build smoke

package vrchat

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"os"
)

const (
	smokeOriginEnvironment = "PROHIBITORUM_VRCHAT_SMOKE_ORIGIN"
	smokeCAEnvironment     = "PROHIBITORUM_VRCHAT_SMOKE_CA_FILE"
)

func resolveOrigin() (originConfig, error) {
	rawOrigin := os.Getenv(smokeOriginEnvironment)
	baseURL, err := url.Parse(rawOrigin)
	if err != nil || rawOrigin == "" || baseURL.Scheme != "https" || baseURL.Host == "" || baseURL.User != nil || baseURL.RawQuery != "" || baseURL.ForceQuery || baseURL.Fragment != "" || baseURL.Path != "/api/1" || !isLoopbackHost(baseURL.Hostname()) {
		return originConfig{}, errors.New("vrchat: invalid smoke origin")
	}
	caFile := os.Getenv(smokeCAEnvironment)
	if caFile == "" {
		return originConfig{}, errors.New("vrchat: missing smoke CA")
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return originConfig{}, errors.New("vrchat: invalid smoke CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return originConfig{}, errors.New("vrchat: invalid smoke CA")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	return originConfig{BaseURL: baseURL, Transport: transport}, nil
}
