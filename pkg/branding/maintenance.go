// Package branding — maintenance.go
//
// The maintenance notice is one text per interface language plus the language
// visitors fall back to. Visitors see their own UI language's notice, or the
// default language's when theirs has none; saving requires the default
// language to have a notice whenever any language does, so a notice is always
// visible to everyone.
package branding

import "errors"

// NoticeLocales is the maintenance-notice language list: exactly the UI
// languages the dashboard supports. Keep it in sync with `locales` in
// dashboard/src/i18n/index.ts — both sides have tests pinning the contents.
var NoticeLocales = []string{"en", "zh"}

// MaxNoticeRunes bounds each language's notice, as before the locales split.
const MaxNoticeRunes = 500

// Maintenance notice validation errors, mapped by the admin settings handler
// to public error codes.
var (
	// ErrNoticeLocale: an unknown language key, or a default language outside
	// NoticeLocales.
	ErrNoticeLocale = errors.New("branding: unknown maintenance notice locale")
	// ErrNoticeTooLong: one language's notice exceeds MaxNoticeRunes.
	ErrNoticeTooLong = errors.New("branding: maintenance notice too long")
	// ErrNoticeDefaultRequired: some language has a notice but the default
	// language does not.
	ErrNoticeDefaultRequired = errors.New("branding: the default language needs a maintenance notice")
)

// MaintenanceNotice is the multilingual maintenance notice.
type MaintenanceNotice struct {
	// Messages holds one non-empty notice per language key; empty strings are
	// never stored.
	Messages map[string]string
	// DefaultLocale is the fallback language; "en" when nothing is saved.
	DefaultLocale string
}

// IsNoticeLocale reports whether locale is a language a notice may be written
// in.
func IsNoticeLocale(locale string) bool {
	for _, l := range NoticeLocales {
		if l == locale {
			return true
		}
	}
	return false
}

// ValidateMaintenanceNotice checks a normalized notice (no empty-string
// entries): every key and the default language must be known languages, each
// text within MaxNoticeRunes, and the default language present whenever any
// notice exists.
func ValidateMaintenanceNotice(n MaintenanceNotice) error {
	if n.DefaultLocale == "" || !IsNoticeLocale(n.DefaultLocale) {
		return ErrNoticeLocale
	}
	for locale, text := range n.Messages {
		if !IsNoticeLocale(locale) {
			return ErrNoticeLocale
		}
		if len([]rune(text)) > MaxNoticeRunes {
			return ErrNoticeTooLong
		}
	}
	if len(n.Messages) > 0 {
		if _, ok := n.Messages[n.DefaultLocale]; !ok {
			return ErrNoticeDefaultRequired
		}
	}
	return nil
}

// normalizeNoticeMessages drops empty-string entries: an empty text is "no
// notice in that language", never a stored row.
func normalizeNoticeMessages(messages map[string]string) map[string]string {
	out := make(map[string]string, len(messages))
	for locale, text := range messages {
		if text != "" {
			out[locale] = text
		}
	}
	return out
}
