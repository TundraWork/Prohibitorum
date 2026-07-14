package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Summary is the subset of a Steam player summary we consume.
type Summary struct {
	PersonaName string
	AvatarURL   string
}

// FetchSummary calls ISteamUser/GetPlayerSummaries and returns the player's persona
// name + full-size avatar URL. apiKey is the Steam Web API key; steamID is 17 digits.
func FetchSummary(ctx context.Context, hc *http.Client, apiKey, steamID string) (Summary, error) {
	u := summaryEndpoint + "?" + url.Values{"key": {apiKey}, "steamids": {steamID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Summary{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Summary{}, fmt.Errorf("steam: player summaries: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Summary{}, fmt.Errorf("steam: player summaries status %d", resp.StatusCode)
	}
	var out struct {
		Response struct {
			Players []struct {
				PersonaName string `json:"personaname"`
				AvatarFull  string `json:"avatarfull"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Summary{}, err
	}
	if len(out.Response.Players) == 0 {
		return Summary{}, errors.New("steam: no player summary returned")
	}
	p := out.Response.Players[0]
	return Summary{PersonaName: p.PersonaName, AvatarURL: p.AvatarFull}, nil
}
