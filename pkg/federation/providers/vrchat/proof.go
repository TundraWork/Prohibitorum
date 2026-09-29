package vrchat

import (
	"errors"
	"net/url"
	"strings"
)

const (
	issuerURL       = "https://api.vrchat.cloud/api/1"
	profileURLBase  = "https://vrchat.com/home/user/"
	proofPathPrefix = "/verify/vrchat/"
)

func parseIdentity(input string) (string, error) {
	if canonicalUserIDPattern.MatchString(input) {
		return input, nil
	}
	if strings.Contains(input, "%") {
		return "", errors.New("vrchat: invalid identity")
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "vrchat.com" || parsed.User != nil || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("vrchat: invalid identity")
	}
	if !strings.HasPrefix(parsed.Path, "/home/user/") {
		return "", errors.New("vrchat: invalid identity")
	}
	userID := strings.TrimPrefix(parsed.Path, "/home/user/")
	if !canonicalUserIDPattern.MatchString(userID) || parsed.Path != "/home/user/"+userID {
		return "", errors.New("vrchat: invalid identity")
	}
	return userID, nil
}

func proofLinkMatches(link string, origin *url.URL, token string) bool {
	if token == "" || strings.Contains(link, "%") {
		return false
	}
	candidate, err := url.Parse(link)
	if err != nil || candidate.User != nil || candidate.RawPath != "" || candidate.RawQuery != "" || candidate.ForceQuery || candidate.Fragment != "" {
		return false
	}
	if !sameEffectiveOrigin(candidate, origin) {
		return false
	}
	return candidate.Path == proofPathPrefix+token
}

func sameEffectiveOrigin(left, right *url.URL) bool {
	if left.Scheme != right.Scheme || (left.Scheme != "http" && left.Scheme != "https") || !strings.EqualFold(left.Hostname(), right.Hostname()) {
		return false
	}
	return effectivePort(left) == effectivePort(right)
}

func effectivePort(value *url.URL) string {
	if value.Port() != "" {
		return value.Port()
	}
	if value.Scheme == "http" {
		return "80"
	}
	return "443"
}
