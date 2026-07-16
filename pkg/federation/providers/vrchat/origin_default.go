//go:build !smoke

package vrchat

import (
	"net/http"
	"net/url"
)

func resolveOrigin() (originConfig, error) {
	baseURL, err := url.Parse(productionOrigin)
	if err != nil {
		return originConfig{}, err
	}
	return originConfig{BaseURL: baseURL, Transport: http.DefaultTransport}, nil
}
