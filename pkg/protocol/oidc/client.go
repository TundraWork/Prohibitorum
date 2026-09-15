package oidc

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"
)

// errInvalidClient is the single sentinel returned for every client
// load/authentication failure. Callers map it to the OAuth `invalid_client`
// error. Collapsing all failure causes into one sentinel avoids leaking
// which check failed (unknown client vs. wrong secret vs. wrong method),
// which would otherwise enable client enumeration. A later task adds the
// full OAuth error set; for now this lives here.
var errInvalidClient = errors.New("oidc: invalid client")

// dummyClientSecretPHC is a fixed argon2id hash using the SAME parameters as
// real client secrets (password.DefaultParams). On the unknown-client path we
// verify the presented secret against it to burn an equivalent argon2id cost,
// so request timing cannot distinguish a known confidential client (which runs
// a full verify) from an unknown one. Computed once at package load.
var dummyClientSecretPHC = func() string {
	phc, err := password.HashRaw("timing-equalizer", password.DefaultParams())
	if err != nil {
		panic("oidc: precompute dummy client secret PHC: " + err.Error())
	}
	return phc
}()

// verifyClientSecret is the secret-verification seam. It is assigned once at
// init and is overridden ONLY by non-parallel tests (no t.Parallel) so the
// swap is race-free; do not add t.Parallel to tests that swap it.
var verifyClientSecret = password.VerifyRaw

// clientQueries is the subset of db.Querier that client.go needs. Mirrors
// the narrow-interface pattern used by keys.go's signingKeyQueries so this
// file stays independently compilable and unit-testable with a fake.
type clientQueries interface {
	GetOIDCClient(ctx context.Context, clientID string) (db.OidcClient, error)
}

// loadClient fetches an enabled oidc_client by ID. The underlying query
// filters `disabled = false`, so disabled or unknown clients surface as
// pgx.ErrNoRows, which is normalized to errInvalidClient.
func loadClient(ctx context.Context, q clientQueries, clientID string) (db.OidcClient, error) {
	c, err := q.GetOIDCClient(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.OidcClient{}, errInvalidClient
		}
		return db.OidcClient{}, err
	}
	return c, nil
}

// basicAuthPrefix is the credentials prefix of an HTTP Basic Authorization
// header (RFC 7617 §2). Matched case-insensitively, as net/http does.
const basicAuthPrefix = "Basic "

// hasBasicAuthHeader reports whether the request carries an HTTP Basic
// Authorization header, WITHOUT requiring it to be well-formed. Callers use
// this to decide whether the Basic channel was attempted — a malformed header
// is still an attempt, so it must earn a WWW-Authenticate challenge rather
// than silently degrading to a challenge-less 401.
func hasBasicAuthHeader(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return len(auth) >= len(basicAuthPrefix) &&
		strings.EqualFold(auth[:len(basicAuthPrefix)], basicAuthPrefix)
}

// parseBasicCredentials extracts and decodes HTTP Basic credentials per
// RFC 6749 §2.3.1. present reports whether the request carried a Basic
// Authorization header at all; ok reports whether that header could be
// decoded.
//
// This exists because the standard library's (*http.Request).BasicAuth only
// base64-decodes and splits on the first colon — it skips the
// application/x-www-form-urlencoded decoding step §2.3.1 mandates. A conforming
// RP percent-encodes its client_id and client_secret before joining them with
// the colon, and some client libraries encode `-` `_` `.` `~` as well, so a
// client_id of `my-client` arrives as `my%2Dclient`. Without this decode it
// misses in the database and the request fails with invalid_client.
//
// Decoding is unconditional: there is no fall back to the raw value when the
// percent-decode fails, so a malformed header is a hard failure. Values with
// no reserved characters survive the round trip unchanged, and the secrets
// this project generates use base64url (`A-Za-z0-9-_`, no `%` or `+`), so the
// decode is a no-op for them.
func parseBasicCredentials(r *http.Request) (id, secret string, present, ok bool) {
	if !hasBasicAuthHeader(r) {
		return "", "", false, true
	}
	encoded := r.Header.Get("Authorization")[len(basicAuthPrefix):]

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", true, false
	}

	rawID, rawSecret, found := strings.Cut(string(decoded), ":")
	if !found {
		return "", "", true, false
	}

	// QueryUnescape, not PathUnescape: §2.3.1 names the
	// application/x-www-form-urlencoded algorithm, under which `+` decodes to
	// a space.
	id, err = url.QueryUnescape(rawID)
	if err != nil {
		return "", "", true, false
	}
	secret, err = url.QueryUnescape(rawSecret)
	if err != nil {
		return "", "", true, false
	}
	return id, secret, true, true
}

// authenticateClient identifies and authenticates the token-endpoint caller.
//
// It extracts the presented client_id and (optional) secret from either an
// HTTP Basic Authorization header or the POST body, loads the client, and
// enforces the client's registered client_auth_method:
//
//   - client_secret: confidential client. The secret may arrive through
//     EITHER channel — Basic (client_secret_basic) and body
//     (client_secret_post) are equivalent, since client_auth_method is only a
//     confidential-vs-public discriminator, not an OIDC Discovery
//     registration value. Verified via constant-time argon2id against the
//     stored PHC hash.
//   - none: public client (no stored hash); requires that NO secret is
//     presented by either channel.
//
// Basic credentials are decoded per RFC 6749 §2.3.1 (see
// parseBasicCredentials). Presenting a secret through both channels at once is
// rejected per RFC 6749 §2.3; repeating a matching client_id in the body
// alongside Basic auth is allowed, because client_id is an identifier rather
// than a credential. Every failure returns errInvalidClient so callers map
// uniformly to invalid_client.
func authenticateClient(ctx context.Context, q clientQueries, r *http.Request) (db.OidcClient, error) {
	if err := r.ParseForm(); err != nil {
		return db.OidcClient{}, errInvalidClient
	}

	basicID, basicSecret, hasBasic, basicOK := parseBasicCredentials(r)
	formID := r.PostForm.Get("client_id")
	formSecret := r.PostForm.Get("client_secret")

	// An undecodable Basic header yields no usable client_id, so bail out ahead
	// of loadClient. Returning here also keeps this path off the argon2id
	// equalizer below — there is no known-vs-unknown client distinction to leak
	// when no client was ever identified, and skipping it denies a caller an
	// argon2id burn that costs them nothing to trigger.
	if hasBasic && !basicOK {
		return db.OidcClient{}, errInvalidClient
	}

	// RFC 6749 §2.3: a client MUST NOT use more than one authentication method
	// per request. A body client_secret alongside Basic auth is exactly that.
	if hasBasic && formSecret != "" {
		return db.OidcClient{}, errInvalidClient
	}
	// client_id is an identifier, not a credential, and many RPs repeat it in
	// the body while authenticating with Basic. Allow the repetition, but the
	// two copies must name the same client. Both sides are compared decoded:
	// basicID came through §2.3.1 decoding and ParseForm already decoded the
	// body.
	if hasBasic && formID != "" && formID != basicID {
		return db.OidcClient{}, errInvalidClient
	}

	var clientID string
	switch {
	case hasBasic:
		clientID = basicID
	default:
		clientID = formID
	}
	if clientID == "" {
		return db.OidcClient{}, errInvalidClient
	}

	client, err := loadClient(ctx, q, clientID)
	if err != nil {
		// Timing-oracle defense: equalize the unknown/disabled-client path with
		// the known-client wrong-secret path (which runs a full argon2id verify).
		// Only for the errInvalidClient case (the actual oracle) and only when a
		// secret was presented — don't burn argon2 on infra errors or on
		// unauthenticated requests. (No net-new DoS surface vs the known-client
		// wrong-secret path, which already accepts unauthenticated argon2 burns.)
		presentedSecret := basicSecret
		if presentedSecret == "" {
			presentedSecret = formSecret
		}
		if presentedSecret != "" && errors.Is(err, errInvalidClient) {
			_ = verifyClientSecret(presentedSecret, dummyClientSecretPHC)
		}
		return db.OidcClient{}, err
	}

	switch client.ClientAuthMethod {
	case "client_secret":
		// Either channel is accepted. The mutual-exclusion check above has
		// already ruled out both carrying a secret at once, so the Basic value
		// wins whenever the header is present.
		presented := formSecret
		if hasBasic {
			presented = basicSecret
		}
		if presented == "" || !client.ClientSecretHash.Valid {
			return db.OidcClient{}, errInvalidClient
		}
		if !verifyClientSecret(presented, client.ClientSecretHash.String) {
			return db.OidcClient{}, errInvalidClient
		}

	case "none":
		// Public client: no secret may be presented by any channel, and the
		// client must not carry a stored hash.
		if client.ClientSecretHash.Valid {
			return db.OidcClient{}, errInvalidClient
		}
		// hasBasic catches Basic headers with an empty password:
		// "Basic base64(user:)" parses cleanly into ("user", "", true, true),
		// so checking only basicSecret != "" would miss that bypass. The
		// relaxation for a repeated body client_id does not reach here — a
		// public client may not send a Basic header at all.
		if hasBasic || basicSecret != "" || formSecret != "" {
			return db.OidcClient{}, errInvalidClient
		}

	default:
		// Anything outside the closed {client_secret, none} vocabulary is dirty
		// data — reject rather than treating "not none" as confidential.
		return db.OidcClient{}, errInvalidClient
	}

	return client, nil
}
