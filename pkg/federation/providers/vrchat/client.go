package vrchat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	productionOrigin    = "https://api.vrchat.cloud/api/1"
	userResponseLimit   = int64(1 << 20)
	verifyResponseLimit = int64(4 << 10)
	requestBodyLimit    = 4 << 10
	verificationCodeMax = 256
)

var canonicalUserIDPattern = regexp.MustCompile(`^usr_[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type originConfig struct {
	BaseURL   *url.URL
	Transport http.RoundTripper
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
}

func NewClient(buildVersion, publicOrigin string) (*Client, error) {
	origin, err := resolveOrigin()
	if err != nil {
		return nil, err
	}
	return newClient(buildVersion, publicOrigin, origin)
}

func newClient(buildVersion, publicOrigin string, origin originConfig) (*Client, error) {
	if origin.BaseURL == nil || (origin.BaseURL.Scheme != "https" && origin.BaseURL.Scheme != "http") || origin.BaseURL.Host == "" || origin.Transport == nil {
		return nil, &ValidationError{Category: "origin configuration"}
	}
	if buildVersion == "(devel)" {
		buildVersion = "dev"
	}
	if buildVersion == "" || strings.ContainsAny(buildVersion, "\r\n") || publicOrigin == "" || strings.ContainsAny(publicOrigin, "\r\n") {
		return nil, &ValidationError{Category: "client identity"}
	}
	base := *origin.BaseURL
	return &Client{
		baseURL:   &base,
		userAgent: "Prohibitorum/" + buildVersion + " " + publicOrigin,
		httpClient: &http.Client{
			Transport:     origin.Transport,
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *Client) Authenticate(ctx context.Context, username, password string) (CurrentUser, []http.Cookie, error) {
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(username)+":"+url.QueryEscape(password)))
	return c.currentUser(ctx, nil, auth)
}

func (c *Client) CurrentUser(ctx context.Context, cookies []http.Cookie) (CurrentUser, []http.Cookie, error) {
	return c.currentUser(ctx, cookies, "")
}

func (c *Client) currentUser(ctx context.Context, cookies []http.Cookie, authorization string) (CurrentUser, []http.Cookie, error) {
	req, err := c.request(ctx, http.MethodGet, "/auth/user", nil, cookies)
	if err != nil {
		return CurrentUser{}, nil, err
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, newCookies, err := c.do(req, cookies)
	if err != nil {
		return CurrentUser{}, nil, err
	}
	defer resp.Body.Close()
	body, err := readBounded(resp.Body, userResponseLimit, "current-user response")
	if err != nil {
		return CurrentUser{}, newCookies, err
	}
	user, err := decodeCurrentUser(body)
	return user, newCookies, err
}

func (c *Client) PublicUser(ctx context.Context, userID string, cookies []http.Cookie) (PublicUser, []http.Cookie, error) {
	if !canonicalUserIDPattern.MatchString(userID) {
		return PublicUser{}, nil, &ValidationError{Category: "user ID"}
	}
	req, err := c.request(ctx, http.MethodGet, "/users/"+userID, nil, cookies)
	if err != nil {
		return PublicUser{}, nil, err
	}
	resp, newCookies, err := c.do(req, cookies)
	if err != nil {
		return PublicUser{}, nil, err
	}
	defer resp.Body.Close()
	body, err := readBounded(resp.Body, userResponseLimit, "public-user response")
	if err != nil {
		return PublicUser{}, newCookies, err
	}
	user, err := decodePublicUser(body, userID)
	return user, newCookies, err
}

func (c *Client) VerifyTwoFactor(ctx context.Context, method, code string, cookies []http.Cookie) ([]http.Cookie, error) {
	var endpoint string
	switch method {
	case "totp":
		endpoint = "/auth/twofactorauth/totp/verify"
	case "emailOtp":
		endpoint = "/auth/twofactorauth/emailotp/verify"
	case "otp":
		endpoint = "/auth/twofactorauth/otp/verify"
	default:
		return nil, &ValidationError{Category: "two-factor method"}
	}
	if len(code) > verificationCodeMax {
		return nil, &OversizeError{Category: "verification code"}
	}
	body, err := json.Marshal(struct {
		Code string `json:"code"`
	}{Code: code})
	if err != nil {
		return nil, &ValidationError{Category: "verification code"}
	}
	if len(body) > requestBodyLimit {
		return nil, &OversizeError{Category: "verification request"}
	}
	req, err := c.request(ctx, http.MethodPost, endpoint, bytes.NewReader(body), cookies)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, newCookies, err := c.do(req, cookies)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	responseBody, err := readBounded(resp.Body, verifyResponseLimit, "verification response")
	if err != nil {
		return newCookies, err
	}
	var result verifyResultWire
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return newCookies, &DecodeError{Category: "verification"}
	}
	if result.Verified == nil || !*result.Verified {
		return newCookies, &VerificationError{}
	}
	return newCookies, nil
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader, cookies []http.Cookie) (*http.Request, error) {
	if err := validateOutboundCookies(c.baseURL, cookies, time.Now()); err != nil {
		return nil, err
	}
	target := *c.baseURL
	target.Path = strings.TrimSuffix(target.Path, "/") + path
	target.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return nil, &ValidationError{Category: "request"}
	}
	req.Header.Set("User-Agent", c.userAgent)
	for i := range cookies {
		req.AddCookie(&cookies[i])
	}
	return req, nil
}

func (c *Client) do(req *http.Request, prior []http.Cookie) (*http.Response, []http.Cookie, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, &RequestError{}
	}
	now := time.Now()
	if err := statusError(resp, now); err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	updates, err := validateResponseCookies(c.baseURL, resp.Header, now)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	cookies, err := mergeCookies(prior, updates, now)
	if err != nil {
		resp.Body.Close()
		return nil, nil, err
	}
	return resp, cookies, nil
}

func readBounded(reader io.Reader, limit int64, category string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, &DecodeError{Category: category}
	}
	if int64(len(body)) > limit {
		return nil, &OversizeError{Category: category}
	}
	return body, nil
}

func statusError(resp *http.Response, now time.Time) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	category := "unexpected_status"
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		category = "authentication"
	case resp.StatusCode == http.StatusTooManyRequests:
		category = "rate_limited"
	case resp.StatusCode >= 500:
		category = "upstream"
	}
	var retryAfter time.Duration
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), now)
	}
	return &HTTPError{Status: resp.StatusCode, RetryAfter: retryAfter, Category: category}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func decodeCurrentUser(body []byte) (CurrentUser, error) {
	var wire currentUserWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return CurrentUser{}, &DecodeError{Category: "current-user"}
	}
	hasID, hasName := wire.ID != nil, wire.DisplayName != nil
	methodsPresent := wire.RequiresTwoFactorAuth != nil
	methodsNull := methodsPresent && bytes.Equal(bytes.TrimSpace(wire.RequiresTwoFactorAuth), []byte("null"))
	var methods []string
	if methodsPresent && !methodsNull {
		if err := json.Unmarshal(wire.RequiresTwoFactorAuth, &methods); err != nil {
			return CurrentUser{}, &DecodeError{Category: "current-user"}
		}
	}
	if hasID || hasName {
		if !hasID || !hasName || !canonicalUserIDPattern.MatchString(*wire.ID) || len(*wire.DisplayName) == 0 || len(*wire.DisplayName) > 256 || len(methods) != 0 {
			return CurrentUser{}, &DecodeError{Category: "current-user"}
		}
		return CurrentUser{ID: *wire.ID, DisplayName: *wire.DisplayName, RequiresTwoFactorAuth: methods}, nil
	}
	if !methodsPresent || methodsNull || len(methods) == 0 || len(methods) > 3 {
		return CurrentUser{}, &DecodeError{Category: "current-user"}
	}
	filtered := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		switch method {
		case "totp", "emailOtp", "otp":
			if _, duplicate := seen[method]; duplicate {
				return CurrentUser{}, &DecodeError{Category: "current-user"}
			}
			seen[method] = struct{}{}
			filtered = append(filtered, method)
		}
	}
	if len(filtered) == 0 {
		return CurrentUser{}, &DecodeError{Category: "current-user"}
	}
	return CurrentUser{RequiresTwoFactorAuth: filtered}, nil
}

func decodePublicUser(body []byte, requestedID string) (PublicUser, error) {
	var wire publicUserWire
	if err := json.Unmarshal(body, &wire); err != nil {
		return PublicUser{}, &DecodeError{Category: "public-user"}
	}
	if wire.ID == nil || wire.DisplayName == nil || wire.CurrentAvatarThumbnailImageURL == nil ||
		!canonicalUserIDPattern.MatchString(*wire.ID) || len(*wire.DisplayName) == 0 || len(*wire.DisplayName) > 256 || len(*wire.CurrentAvatarThumbnailImageURL) == 0 || len(*wire.CurrentAvatarThumbnailImageURL) > 4096 || wire.BioLinks == nil || bytes.Equal(bytes.TrimSpace(wire.BioLinks), []byte("null")) {
		return PublicUser{}, &DecodeError{Category: "public-user"}
	}
	if *wire.ID != requestedID {
		return PublicUser{}, &IdentityMismatchError{}
	}
	var links []string
	if err := json.Unmarshal(wire.BioLinks, &links); err != nil || links == nil || len(links) > 16 {
		return PublicUser{}, &DecodeError{Category: "public-user"}
	}
	for _, link := range links {
		if len(link) > 2048 {
			return PublicUser{}, &DecodeError{Category: "public-user"}
		}
	}
	return PublicUser{ID: *wire.ID, DisplayName: *wire.DisplayName, BioLinks: links, CurrentAvatarThumbnailImageURL: *wire.CurrentAvatarThumbnailImageURL}, nil
}
