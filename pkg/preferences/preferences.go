// Package preferences is an account's own settings document. It is split into
// sections, one per kind of preference, so a client that predates a section
// never overwrites it: a write replaces only the sections it carries. Only
// sections the user has set are stored; an unset field is null.
package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"prohibitorum/pkg/branding"
)

// Light and dark modes. System follows the device.
const (
	ModeLight  = "light"
	ModeDark   = "dark"
	ModeSystem = "system"
)

var modes = []string{ModeLight, ModeDark, ModeSystem}

// Preferences is every section, each with its fields null when unset.
type Preferences struct {
	Appearance AppearancePreferences `json:"appearance"`
}

// AppearancePreferences is the colour scheme and the light or dark mode. Null
// falls back to this browser's choice, then to the instance default.
type AppearancePreferences struct {
	Scheme *string `json:"scheme" nullable:"true" doc:"A colour scheme slug, or null when not chosen."`
	Mode   *string `json:"mode" nullable:"true" doc:"light, dark or system, or null when not chosen."`
}

// PreferencesPatch carries the sections a write replaces; an absent one is kept.
type PreferencesPatch struct {
	Appearance *AppearancePreferences `json:"appearance,omitempty"`
}

// ErrInvalid wraps every validation failure.
var ErrInvalid = errors.New("preferences: invalid")

// Validate checks every field present in p against its allowed values.
func (p PreferencesPatch) Validate() error {
	if a := p.Appearance; a != nil {
		if a.Scheme != nil && !branding.ValidScheme(*a.Scheme) {
			return fmt.Errorf("%w: scheme", ErrInvalid)
		}
		if a.Mode != nil && !slices.Contains(modes, *a.Mode) {
			return fmt.Errorf("%w: mode", ErrInvalid)
		}
	}
	return nil
}

// Sections renders the sections p carries, ready to merge over the stored
// document.
func (p PreferencesPatch) Sections() ([]byte, error) {
	return json.Marshal(p)
}

// Decode reads a stored document. Values written under an older release that
// are no longer valid, such as a retired scheme, read as unset rather than
// failing the whole document.
func Decode(raw []byte) Preferences {
	var stored PreferencesPatch
	if len(raw) == 0 || json.Unmarshal(raw, &stored) != nil {
		return Preferences{}
	}
	var d Preferences
	if a := stored.Appearance; a != nil {
		if a.Scheme != nil && branding.ValidScheme(*a.Scheme) {
			d.Appearance.Scheme = a.Scheme
		}
		if a.Mode != nil && slices.Contains(modes, *a.Mode) {
			d.Appearance.Mode = a.Mode
		}
	}
	return d
}
