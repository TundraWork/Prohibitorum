package branding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"
)

// Background sources for the sign-in page.
const (
	SourceNone     = "none"
	SourceColor    = "color"
	SourceGradient = "gradient"
	SourceBing     = "bing"
	SourceUnsplash = "unsplash"
	SourceImages   = "images"
)

// MaxLoginImages caps how many background images an instance can upload.
const MaxLoginImages = 10

// Gradients are the preset IDs; the dashboard's gradients.ts draws them.
var Gradients = []string{"dawn", "lagoon", "aurora", "dusk", "mist", "ember"}

// BingMarkets are the markets Bing's daily picture is published for.
var BingMarkets = []string{
	"de-DE", "en-AU", "en-CA", "en-GB", "en-IN", "en-US", "es-ES",
	"fr-CA", "fr-FR", "it-IT", "ja-JP", "pt-BR", "zh-CN",
}

// Horizontal positions of the sign-in card on wide windows.
const (
	CardLeft   = "left"
	CardCenter = "center"
	CardRight  = "right"
)

// Themes for the public pages: the visitor's choice, or always light or dark.
const (
	ThemeSwitchable = "switchable"
	ThemeLight      = "light"
	ThemeDark       = "dark"
)

var sources = []string{SourceNone, SourceColor, SourceGradient, SourceBing, SourceUnsplash, SourceImages}

var (
	cardPositions = []string{CardLeft, CardCenter, CardRight}
	themes        = []string{ThemeSwitchable, ThemeLight, ThemeDark}
)

var hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// Appearance is the sign-in page's look. Every field is always present,
// whichever source is selected, so switching source keeps the others' settings.
type Appearance struct {
	Background Background `json:"background"`
	Card       Surface    `json:"card"`
	Capsules   Surface    `json:"capsules"`
	// CardPosition places the card on wide windows; narrow ones centre it.
	CardPosition string `json:"cardPosition"`
	// Theme applies to the public pages only, never to the console.
	Theme string `json:"theme"`
}

type Background struct {
	Source   string          `json:"source"`
	Color    string          `json:"color"`
	Gradient string          `json:"gradient"`
	Bing     BingOptions     `json:"bing"`
	Unsplash UnsplashOptions `json:"unsplash"`
	Images   ImageOptions    `json:"images"`
}

type BingOptions struct {
	Market      string `json:"market"`
	ShowCaption bool   `json:"showCaption"`
}

type UnsplashOptions struct {
	Query string `json:"query"`
}

type ImageOptions struct {
	Order           string `json:"order"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

// Surface is how the sign-in card or the toolbar capsules are drawn over the
// background. Opacity is a percentage and only applies when Translucent.
type Surface struct {
	Translucent bool `json:"translucent"`
	Opacity     int  `json:"opacity"`
	Blur        bool `json:"blur"`
}

// DefaultAppearance is the look of an instance that has never saved one: the
// page's own background, an opaque centred card, translucent frosted capsules,
// and the theme left to the visitor.
func DefaultAppearance() Appearance {
	return Appearance{
		Background: Background{
			Source:   SourceNone,
			Color:    "#1f6f8b",
			Gradient: "lagoon",
			Bing:     BingOptions{Market: "zh-CN", ShowCaption: true},
			Unsplash: UnsplashOptions{Query: ""},
			Images:   ImageOptions{Order: "random", IntervalSeconds: 10},
		},
		Card:         Surface{Translucent: false, Opacity: 80, Blur: true},
		Capsules:     Surface{Translucent: true, Opacity: 70, Blur: true},
		CardPosition: CardCenter,
		Theme:        ThemeSwitchable,
	}
}

// ErrInvalidAppearance wraps every decoding and validation failure.
var ErrInvalidAppearance = errors.New("branding: invalid sign-in page appearance")

// The wire mirror decodes into pointers so a missing field is told apart from
// a zero value.
type wireAppearance struct {
	Background *struct {
		Source   *string `json:"source"`
		Color    *string `json:"color"`
		Gradient *string `json:"gradient"`
		Bing     *struct {
			Market      *string `json:"market"`
			ShowCaption *bool   `json:"showCaption"`
		} `json:"bing"`
		Unsplash *struct {
			Query *string `json:"query"`
		} `json:"unsplash"`
		Images *struct {
			Order           *string `json:"order"`
			IntervalSeconds *int    `json:"intervalSeconds"`
		} `json:"images"`
	} `json:"background"`
	Card         *wireSurface `json:"card"`
	Capsules     *wireSurface `json:"capsules"`
	CardPosition *string      `json:"cardPosition"`
	Theme        *string      `json:"theme"`
}

type wireSurface struct {
	Translucent *bool `json:"translucent"`
	Opacity     *int  `json:"opacity"`
	Blur        *bool `json:"blur"`
}

// DecodeAppearance strictly decodes raw: unknown fields, missing fields,
// trailing data and out-of-range values are all rejected.
func DecodeAppearance(raw []byte) (Appearance, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireAppearance
	if err := dec.Decode(&w); err != nil {
		return Appearance{}, fmt.Errorf("%w: %v", ErrInvalidAppearance, err)
	}
	if dec.More() {
		return Appearance{}, fmt.Errorf("%w: trailing data", ErrInvalidAppearance)
	}
	a, ok := w.complete()
	if !ok {
		return Appearance{}, fmt.Errorf("%w: missing field", ErrInvalidAppearance)
	}
	if err := ValidateAppearance(a); err != nil {
		return Appearance{}, err
	}
	return a, nil
}

func (w wireAppearance) complete() (Appearance, bool) {
	b := w.Background
	if b == nil || b.Source == nil || b.Color == nil || b.Gradient == nil ||
		b.Bing == nil || b.Bing.Market == nil || b.Bing.ShowCaption == nil ||
		b.Unsplash == nil || b.Unsplash.Query == nil ||
		b.Images == nil || b.Images.Order == nil || b.Images.IntervalSeconds == nil ||
		w.CardPosition == nil || w.Theme == nil {
		return Appearance{}, false
	}
	card, ok := w.Card.complete()
	if !ok {
		return Appearance{}, false
	}
	capsules, ok := w.Capsules.complete()
	if !ok {
		return Appearance{}, false
	}
	return Appearance{
		Background: Background{
			Source:   *b.Source,
			Color:    *b.Color,
			Gradient: *b.Gradient,
			Bing:     BingOptions{Market: *b.Bing.Market, ShowCaption: *b.Bing.ShowCaption},
			Unsplash: UnsplashOptions{Query: *b.Unsplash.Query},
			Images:   ImageOptions{Order: *b.Images.Order, IntervalSeconds: *b.Images.IntervalSeconds},
		},
		Card:         card,
		Capsules:     capsules,
		CardPosition: *w.CardPosition,
		Theme:        *w.Theme,
	}, true
}

func (w *wireSurface) complete() (Surface, bool) {
	if w == nil || w.Translucent == nil || w.Opacity == nil || w.Blur == nil {
		return Surface{}, false
	}
	return Surface{Translucent: *w.Translucent, Opacity: *w.Opacity, Blur: *w.Blur}, true
}

// ValidateAppearance checks every value against its allowed set or range.
func ValidateAppearance(a Appearance) error {
	b := a.Background
	switch {
	case !slices.Contains(sources, b.Source):
		return fmt.Errorf("%w: source", ErrInvalidAppearance)
	case !hexColor.MatchString(b.Color):
		return fmt.Errorf("%w: color", ErrInvalidAppearance)
	case !slices.Contains(Gradients, b.Gradient):
		return fmt.Errorf("%w: gradient", ErrInvalidAppearance)
	case !ValidBingMarket(b.Bing.Market):
		return fmt.Errorf("%w: market", ErrInvalidAppearance)
	case !ValidUnsplashQuery(b.Unsplash.Query):
		return fmt.Errorf("%w: query", ErrInvalidAppearance)
	case b.Images.Order != "random" && b.Images.Order != "carousel":
		return fmt.Errorf("%w: order", ErrInvalidAppearance)
	case b.Images.IntervalSeconds < 5 || b.Images.IntervalSeconds > 3600:
		return fmt.Errorf("%w: interval", ErrInvalidAppearance)
	case !validSurface(a.Card) || !validSurface(a.Capsules):
		return fmt.Errorf("%w: opacity", ErrInvalidAppearance)
	case !slices.Contains(cardPositions, a.CardPosition):
		return fmt.Errorf("%w: card position", ErrInvalidAppearance)
	case !slices.Contains(themes, a.Theme):
		return fmt.Errorf("%w: theme", ErrInvalidAppearance)
	}
	return nil
}

func validSurface(s Surface) bool { return s.Opacity >= 0 && s.Opacity <= 100 }

// ValidBingMarket reports whether m is a market Bing publishes a picture for.
func ValidBingMarket(m string) bool { return slices.Contains(BingMarkets, m) }

// ValidUnsplashQuery accepts 0–64 characters with no surrounding whitespace
// and no control characters. The query is never trimmed on the admin's behalf.
func ValidUnsplashQuery(q string) bool {
	if !utf8.ValidString(q) || utf8.RuneCountInString(q) > 64 {
		return false
	}
	for _, r := range q {
		if unicode.IsControl(r) {
			return false
		}
	}
	if q == "" {
		return true
	}
	first, _ := utf8.DecodeRuneInString(q)
	last, _ := utf8.DecodeLastRuneInString(q)
	return !unicode.IsSpace(first) && !unicode.IsSpace(last)
}
