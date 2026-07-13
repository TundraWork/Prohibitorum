// Command steammock is a tiny mock Steam OpenID 2.0 + Web API server used
// exclusively by the CI smoke test (mise run ci:smoke) to exercise the Steam
// federation login arc without hitting the real Steam network.
//
// It implements three routes:
//
//   - GET  /openid/login  (checkid_setup)   — 302 to openid.return_to + appended id_res params
//   - POST /openid/login  (check_auth)       — responds is_valid:true
//   - GET  /ISteamUser/GetPlayerSummaries/v2/ — returns a canned JSON player summary
//   - GET  /avatar.png                        — returns a minimal valid PNG (avatar inherit)
//
// Usage:
//
//	cmd/steammock --addr 127.0.0.1:18099
//
// The port is intentionally well outside the 18080/18081 dev-federation ports
// and the 8080 smoke port so there is no collision in CI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
)

const steamID = "76561198000000001"

// A fixed 17-digit SteamID64 in the exact format that claimedIDRe validates.
const claimedID = "https://steamcommunity.com/openid/id/" + steamID

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "listen address (host:port); 0 = OS-assigned port")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/openid/login", handleOpenID)
	mux.HandleFunc("/ISteamUser/GetPlayerSummaries/v2/", handleSummaries)
	mux.HandleFunc("/avatar.png", handleAvatar)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("steammock: listen %s: %v", *addr, err)
	}

	// Print the actual address (useful when addr has port=0) to stdout so the
	// caller can capture it. The mise task passes a fixed port, but printing
	// makes the tool self-documenting for ad-hoc use.
	fmt.Fprintln(os.Stdout, ln.Addr().String())

	log.Printf("steammock: listening on %s", ln.Addr())
	if err := http.Serve(ln, mux); err != nil {
		log.Fatalf("steammock: serve: %v", err)
	}
}

// handleOpenID handles both the checkid_setup redirect and the check_authentication
// verification POST. Steam uses the same endpoint for both; the openid.mode param
// distinguishes them.
func handleOpenID(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleCheckidSetup(w, r)
	case http.MethodPost:
		handleCheckAuthentication(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCheckidSetup handles GET /openid/login?openid.mode=checkid_setup.
// It reads openid.return_to and redirects to it with the crafted id_res params
// appended as query parameters.
func handleCheckidSetup(w http.ResponseWriter, r *http.Request) {
	returnTo := r.URL.Query().Get("openid.return_to")
	if returnTo == "" {
		http.Error(w, "missing openid.return_to", http.StatusBadRequest)
		return
	}

	// The Federator's steamCallbackURL already includes the state token as a
	// query param: .../callback?state=<token>. We must append the openid.*
	// params without clobbering the existing query.
	u, err := url.Parse(returnTo)
	if err != nil {
		http.Error(w, "bad return_to", http.StatusBadRequest)
		return
	}
	q := u.Query()
	q.Set("openid.ns", "http://specs.openid.net/auth/2.0")
	q.Set("openid.mode", "id_res")
	q.Set("openid.return_to", returnTo)
	q.Set("openid.claimed_id", claimedID)
	q.Set("openid.identity", claimedID)
	q.Set("openid.sig", "mocksig")
	q.Set("openid.signed", "mode,claimed_id,identity,return_to")
	u.RawQuery = q.Encode()

	log.Printf("steammock: checkid_setup → 302 %s", u.String())
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// handleCheckAuthentication handles POST /openid/login with openid.mode=check_authentication.
// The Steam adapter sends all callback params back verbatim with mode flipped. We
// always respond is_valid:true (this is a mock — real signature verification is
// out of scope for smoke testing).
func handleCheckAuthentication(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ns:http://specs.openid.net/auth/2.0\nis_valid:true\n")
	log.Printf("steammock: check_authentication → is_valid:true")
}

// handleSummaries handles GET /ISteamUser/GetPlayerSummaries/v2/ and returns a
// canned JSON response for the fixed SteamID64. The smoke asserts on personaname.
func handleSummaries(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"response": map[string]any{
			"players": []map[string]any{
				{
					"steamid":     steamID,
					"personaname": "SmokeGaben",
					// avatarfull must be a reachable URL; the avatar-inherit goroutine
					// will fetch it. Use the same mock host (the smoke seeds the
					// upstream IdP with allow_private_network=true so localhost URLs
					// are permitted).
					"avatarfull": "http://" + r.Host + "/avatar.png",
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("steammock: summaries encode: %v", err)
	}
	log.Printf("steammock: GetPlayerSummaries → personaname=SmokeGaben")
}

// handleAvatar serves a tiny but valid 4×4 PNG so the avatar-inherit goroutine
// does not error. The smoke does not assert on pixel content, only that the
// account ends up with an avatar_url from the Steam upstream row.
func handleAvatar(w http.ResponseWriter, r *http.Request) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	w.Header().Set("Content-Type", "image/png")
	if err := png.Encode(w, img); err != nil {
		log.Printf("steammock: avatar encode: %v", err)
	}
	log.Printf("steammock: /avatar.png served")
}
