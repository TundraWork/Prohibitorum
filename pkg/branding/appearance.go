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
	SourceNone      = "none"
	SourceColor     = "color"
	SourceGradient  = "gradient"
	SourceBing      = "bing"
	SourceUnsplash  = "unsplash"
	SourcePixabay   = "pixabay"
	SourceOpenverse = "openverse"
	SourceImages    = "images"
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

// PixabayLanguages are the language codes Pixabay's search accepts, as its
// API docs list them. Pixabay itself does not validate the parameter, so the
// server does. The dashboard's language picker shows the same codes.
var PixabayLanguages = []string{
	"cs", "da", "de", "en", "es", "fr", "id", "it", "hu", "nl", "no", "pl",
	"pt", "ro", "sk", "fi", "sv", "tr", "vi", "th", "bg", "ru", "el", "ja",
	"ko", "zh",
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

// Schemes are the colour schemes' slugs, in the order the pickers list them.
// The dashboard's lib/color-scheme.ts registers the same ones; a test keeps
// the two lists equal.
var Schemes = []string{"graphite", "nord", "sand", "matcha", "sakura", "lavender", "lagoon", "citrus",
	"catppuccin", "catppuccin-gray",
}

// DefaultScheme is the colour scheme of an instance that has not chosen one.
const DefaultScheme = "graphite"

var sources = []string{SourceNone, SourceColor, SourceGradient, SourceBing, SourceUnsplash, SourcePixabay, SourceOpenverse, SourceImages}

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
	// Scheme is the colour scheme every page uses until the visitor or the
	// signed-in user picks another.
	Scheme string `json:"scheme"`
}

type Background struct {
	Source    string           `json:"source"`
	Color     string           `json:"color"`
	Gradient  string           `json:"gradient"`
	Bing      BingOptions      `json:"bing"`
	Unsplash  UnsplashOptions  `json:"unsplash"`
	Pixabay   PixabayOptions   `json:"pixabay"`
	Openverse OpenverseOptions `json:"openverse"`
	Images    ImageOptions     `json:"images"`
}

type BingOptions struct {
	Market      string `json:"market"`
	ShowCaption bool   `json:"showCaption"`
}

type UnsplashOptions struct {
	Query           string `json:"query"`
	Order           string `json:"order"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

// PixabayOptions holds the Pixabay search: Lang is the language the keyword is
// matched in (one of PixabayLanguages), so the keyword must be written in it.
type PixabayOptions struct {
	Query           string `json:"query"`
	Lang            string `json:"lang"`
	Order           string `json:"order"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

// OpenverseOptions holds the Openverse search. There is no key: the API is
// used anonymously, so only the keyword (required once Openverse is chosen),
// the order and the interval are set.
type OpenverseOptions struct {
	Query           string `json:"query"`
	Order           string `json:"order"`
	IntervalSeconds int    `json:"intervalSeconds"`
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
			Source:    SourceNone,
			Color:     "#1f6f8b",
			Gradient:  "lagoon",
			Bing:      BingOptions{Market: "zh-CN", ShowCaption: true},
			Unsplash:  UnsplashOptions{Query: "", Order: "random", IntervalSeconds: 10},
			Pixabay:   PixabayOptions{Query: "", Lang: "zh", Order: "random", IntervalSeconds: 10},
			Openverse: OpenverseOptions{Query: "", Order: "random", IntervalSeconds: 10},
			Images:    ImageOptions{Order: "random", IntervalSeconds: 10},
		},
		Card:         Surface{Translucent: false, Opacity: 80, Blur: true},
		Capsules:     Surface{Translucent: true, Opacity: 70, Blur: true},
		CardPosition: CardCenter,
		Theme:        ThemeSwitchable,
		Scheme:       DefaultScheme,
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
			Query           *string `json:"query"`
			Order           *string `json:"order"`
			IntervalSeconds *int    `json:"intervalSeconds"`
		} `json:"unsplash"`
		Pixabay *struct {
			Query           *string `json:"query"`
			Lang            *string `json:"lang"`
			Order           *string `json:"order"`
			IntervalSeconds *int    `json:"intervalSeconds"`
		} `json:"pixabay"`
		Openverse *struct {
			Query           *string `json:"query"`
			Order           *string `json:"order"`
			IntervalSeconds *int    `json:"intervalSeconds"`
		} `json:"openverse"`
		Images *struct {
			Order           *string `json:"order"`
			IntervalSeconds *int    `json:"intervalSeconds"`
		} `json:"images"`
	} `json:"background"`
	Card         *wireSurface `json:"card"`
	Capsules     *wireSurface `json:"capsules"`
	CardPosition *string      `json:"cardPosition"`
	Theme        *string      `json:"theme"`
	Scheme       *string      `json:"scheme"`
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
		b.Unsplash.Order == nil || b.Unsplash.IntervalSeconds == nil ||
		b.Pixabay == nil || b.Pixabay.Query == nil || b.Pixabay.Lang == nil ||
		b.Pixabay.Order == nil || b.Pixabay.IntervalSeconds == nil ||
		b.Openverse == nil || b.Openverse.Query == nil ||
		b.Openverse.Order == nil || b.Openverse.IntervalSeconds == nil ||
		b.Images == nil || b.Images.Order == nil || b.Images.IntervalSeconds == nil ||
		w.CardPosition == nil || w.Theme == nil || w.Scheme == nil {
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
			Unsplash: UnsplashOptions{
				Query:           *b.Unsplash.Query,
				Order:           *b.Unsplash.Order,
				IntervalSeconds: *b.Unsplash.IntervalSeconds,
			},
			Pixabay: PixabayOptions{
				Query:           *b.Pixabay.Query,
				Lang:            *b.Pixabay.Lang,
				Order:           *b.Pixabay.Order,
				IntervalSeconds: *b.Pixabay.IntervalSeconds,
			},
			Openverse: OpenverseOptions{
				Query:           *b.Openverse.Query,
				Order:           *b.Openverse.Order,
				IntervalSeconds: *b.Openverse.IntervalSeconds,
			},
			Images: ImageOptions{Order: *b.Images.Order, IntervalSeconds: *b.Images.IntervalSeconds},
		},
		Card:         card,
		Capsules:     capsules,
		CardPosition: *w.CardPosition,
		Theme:        *w.Theme,
		Scheme:       *w.Scheme,
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
	case !ValidWallpaperQuery(b.Unsplash.Query):
		return fmt.Errorf("%w: query", ErrInvalidAppearance)
	case !ValidWallpaperQuery(b.Pixabay.Query) || !ValidPixabayLang(b.Pixabay.Lang):
		return fmt.Errorf("%w: pixabay query or language", ErrInvalidAppearance)
	case !ValidWallpaperQuery(b.Openverse.Query):
		return fmt.Errorf("%w: query", ErrInvalidAppearance)
	case !validSurface(a.Card) || !validSurface(a.Capsules):
		return fmt.Errorf("%w: opacity", ErrInvalidAppearance)
	case !slices.Contains(cardPositions, a.CardPosition):
		return fmt.Errorf("%w: card position", ErrInvalidAppearance)
	case !slices.Contains(themes, a.Theme):
		return fmt.Errorf("%w: theme", ErrInvalidAppearance)
	case !ValidScheme(a.Scheme):
		return fmt.Errorf("%w: scheme", ErrInvalidAppearance)
	}
	if field, ok := validRotation(b.Images.Order, b.Images.IntervalSeconds); !ok {
		return fmt.Errorf("%w: %s", ErrInvalidAppearance, field)
	}
	if field, ok := validRotation(b.Unsplash.Order, b.Unsplash.IntervalSeconds); !ok {
		return fmt.Errorf("%w: %s", ErrInvalidAppearance, field)
	}
	if field, ok := validRotation(b.Pixabay.Order, b.Pixabay.IntervalSeconds); !ok {
		return fmt.Errorf("%w: %s", ErrInvalidAppearance, field)
	}
	if field, ok := validRotation(b.Openverse.Order, b.Openverse.IntervalSeconds); !ok {
		return fmt.Errorf("%w: %s", ErrInvalidAppearance, field)
	}
	// Openverse has no "any picture" mode worth showing, so its keyword is
	// required once it is the chosen source; unchosen, an empty one stays
	// valid so the defaults and the migration's filled-in object pass.
	if b.Source == SourceOpenverse && b.Openverse.Query == "" {
		return fmt.Errorf("%w: query", ErrInvalidAppearance)
	}
	return nil
}

// validRotation checks how a source with several pictures shows them: one at
// random per visit, or a carousel changing every 5–3600 seconds. It names the
// field at fault.
func validRotation(order string, interval int) (field string, ok bool) {
	switch {
	case order != "random" && order != "carousel":
		return "order", false
	case interval < 5 || interval > 3600:
		return "interval", false
	}
	return "", true
}

func validSurface(s Surface) bool { return s.Opacity >= 0 && s.Opacity <= 100 }

// ValidScheme reports whether s is a colour scheme's slug.
func ValidScheme(s string) bool { return slices.Contains(Schemes, s) }

// ValidBingMarket reports whether m is a market Bing publishes a picture for.
func ValidBingMarket(m string) bool { return slices.Contains(BingMarkets, m) }

// ValidWallpaperQuery accepts 0–64 characters with no surrounding whitespace
// and no control characters, for any picture service's keyword. The query is
// never trimmed on the admin's behalf.
func ValidWallpaperQuery(q string) bool {
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

// ValidPixabayLang reports whether lang is one of Pixabay's search languages.
func ValidPixabayLang(lang string) bool { return slices.Contains(PixabayLanguages, lang) }
