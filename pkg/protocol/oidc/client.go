package oidc

import (
	"context"
	"errors"
	"net/http"

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

// authenticateClient identifies and authenticates the token-endpoint caller.
//
// It extracts the presented client_id and (optional) secret from either an
// HTTP Basic Authorization header (client_secret_basic) or the POST body
// (client_secret_post / none), loads the client, and enforces that the
// presentation style matches the client's registered
// token_endpoint_auth_method:
//
//   - client_secret_basic: requires Basic auth with a non-empty secret,
//     verified via constant-time argon2id against the stored PHC hash.
//   - client_secret_post:  requires a non-empty form client_secret,
//     verified the same way.
//   - none:                public client (no stored hash); requires that NO
//     secret is presented by either channel.
//
// Presenting credentials via both Basic and POST simultaneously is rejected
// per RFC 6749 §2.3. Every failure returns errInvalidClient so callers map
// uniformly to invalid_client.
func authenticateClient(ctx context.Context, q clientQueries, r *http.Request) (db.OidcClient, error) {
	if err := r.ParseForm(); err != nil {
		return db.OidcClient{}, errInvalidClient
	}

	basicID, basicSecret, hasBasic := r.BasicAuth()
	formID := r.PostForm.Get("client_id")
	formSecret := r.PostForm.Get("client_secret")

	// RFC 6749 §2.3: a client MUST NOT use more than one authentication
	// method per request. Reject simultaneous Basic + POST credentials.
	if hasBasic && (formSecret != "" || formID != "") {
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

	switch client.TokenEndpointAuthMethod {
	case "client_secret_basic":
		// Must arrive via Basic with a non-empty secret; no form secret.
		if !hasBasic || basicSecret == "" || formSecret != "" {
			return db.OidcClient{}, errInvalidClient
		}
		if !client.ClientSecretHash.Valid {
			return db.OidcClient{}, errInvalidClient
		}
		if !verifyClientSecret(basicSecret, client.ClientSecretHash.String) {
			return db.OidcClient{}, errInvalidClient
		}

	case "client_secret_post":
		// Must arrive via the form with a non-empty secret; no Basic header.
		if hasBasic || formSecret == "" {
			return db.OidcClient{}, errInvalidClient
		}
		if !client.ClientSecretHash.Valid {
			return db.OidcClient{}, errInvalidClient
		}
		if !verifyClientSecret(formSecret, client.ClientSecretHash.String) {
			return db.OidcClient{}, errInvalidClient
		}

	case "none":
		// Public client: no secret may be presented by any channel, and the
		// client must not carry a stored hash.
		if client.ClientSecretHash.Valid {
			return db.OidcClient{}, errInvalidClient
		}
		// hasBasic catches Basic headers with an empty password: Go's
		// r.BasicAuth() returns ("user", "", true) for "Basic base64(user:)",
		// so checking only basicSecret != "" would miss that bypass.
		if hasBasic || basicSecret != "" || formSecret != "" {
			return db.OidcClient{}, errInvalidClient
		}

	default:
		return db.OidcClient{}, errInvalidClient
	}

	return client, nil
}
