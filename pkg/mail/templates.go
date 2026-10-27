package mail

import (
	"bytes"
	"embed"
	"fmt"
	"net/http"
	"strings"

	htmltemplate "html/template"
	texttemplate "text/template"
)

// Locale selects the copy pair. Anything unrecognized falls back to en,
// so a request carrying "ja" still gets a usable mail.
type Locale string

const (
	LocaleZh Locale = "zh"
	LocaleEn Locale = "en"
)

// NormalizeLocale maps an arbitrary request locale onto the two supported
// ones, defaulting to en.
func NormalizeLocale(s string) Locale {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "zh-cn", "zh-hans", "zh_cn":
		return LocaleZh
	default:
		return LocaleEn
	}
}

//go:embed templates/layout.html.tmpl
var templateFS embed.FS

var layoutTmpl = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/layout.html.tmpl"))

// Brand carries what the mail needs from the instance's branding settings:
// the effective instance name and the effective icon's raw bytes (as served
// by branding.Resolver.Icon; empty Icon means no image in the mail).
type Brand struct {
	Name string
	Icon []byte
}

// iconCID is the Content-ID the layout's <img> references (cid:instance-icon).
const iconCID = "instance-icon"

// msoTableOpen/Close wrap the content in a fixed 480px table for Outlook for
// Windows, which ignores max-width. html/template strips HTML comments and
// mangles non-http URLs, so both arrive as fixed template.HTML constants —
// no user input flows into them. Same for iconURL: the only src the layout
// ever carries.
var (
	msoTableOpen  = htmltemplate.HTML(`<!--[if mso]><table role="presentation" cellpadding="0" cellspacing="0" border="0" width="480"><tr><td><![endif]-->`)
	msoTableClose = htmltemplate.HTML(`<!--[if mso]></td></tr></table><![endif]-->`)
	iconURL       = htmltemplate.URL("cid:" + iconCID)
)

// mailCopy holds one language's copy for either mail. Every field is a
// text/template fragment over .Instance/.Minutes; both locales carry the
// same fields, so a missing one fails the completeness test at build time.
type mailCopy struct {
	Subject   string // mail subject; carries the instance name, never the code
	Preview   string // hidden preview text; never the code
	Heading   string // in-body heading
	Lead      string // body intro naming the instance
	CodeLabel string // plain-text label before the code (code mail only)
	Validity  string // code validity line (code mail only)
}

var codeCopy = map[Locale]mailCopy{
	LocaleZh: {
		Subject:   "{{.Instance}} 邮箱验证码",
		Preview:   "验证码 {{.Minutes}} 分钟内有效。",
		Heading:   "邮箱验证码",
		Lead:      "{{.Instance}} 上的一个账户正在验证此邮箱地址。",
		CodeLabel: "验证码",
		Validity:  "{{.Minutes}} 分钟内有效。",
	},
	LocaleEn: {
		Subject:   "{{.Instance}} email verification code",
		Preview:   "The code is valid for {{.Minutes}} minutes.",
		Heading:   "Email verification code",
		Lead:      "An account on {{.Instance}} is verifying this email address.",
		CodeLabel: "Verification code",
		Validity:  "Valid for {{.Minutes}} minutes.",
	},
}

var testCopy = map[Locale]mailCopy{
	LocaleZh: {
		Subject: "{{.Instance}} 测试邮件",
		Preview: "发信配置可以送达此地址。",
		Heading: "测试邮件",
		Lead:    "{{.Instance}} 的发信配置可以送达此地址。",
	},
	LocaleEn: {
		Subject: "{{.Instance}} test mail",
		Preview: "Outbound mail reaches this address.",
		Heading: "Test mail",
		Lead:    "Outbound mail from {{.Instance}} reaches this address.",
	},
}

// copyVars feeds the text/template copy fragments.
type copyVars struct {
	Instance string
	Minutes  int
}

func renderCopy(frag, instance string, minutes int) (string, error) {
	t, err := texttemplate.New("copy").Parse(frag)
	if err != nil {
		return "", fmt.Errorf("mail: copy: %w", err)
	}
	var b strings.Builder
	if err := t.Execute(&b, copyVars{Instance: instance, Minutes: minutes}); err != nil {
		return "", fmt.Errorf("mail: copy: %w", err)
	}
	return b.String(), nil
}

// layoutData fills templates/layout.html.tmpl. MSOTableOpen/Close and IconSrc
// are the pre-validated constants above; every other string was already
// rendered from the copy tables and is escaped by html/template here.
type layoutData struct {
	Lang          string
	Title         string
	Preheader     string
	MSOTableOpen  htmltemplate.HTML
	MSOTableClose htmltemplate.HTML
	IconSrc       htmltemplate.URL
	Instance      string
	Heading       string
	Lead          string
	Code          string
	Validity      string
}

// inlineFromIcon returns the CID embed for the icon's raw bytes, or nil when
// the bytes are empty or not a supported image type — the mail then goes out
// without an image rather than without delivery. No conversion: the bytes are
// embedded exactly as branding serves them (WebP today; PNG/JPEG/GIF for
// older rows and the built-in PNG).
func inlineFromIcon(icon []byte) *Inline {
	if len(icon) == 0 {
		return nil
	}
	ctype := http.DetectContentType(icon)
	var ext string
	switch ctype {
	case "image/webp":
		ext = "webp"
	case "image/png":
		ext = "png"
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	default:
		return nil
	}
	return &Inline{
		ContentID:   iconCID,
		Filename:    "icon." + ext,
		ContentType: ctype,
		Data:        icon,
	}
}

// renderMail builds a Message: copy fragments render once via text/template,
// then feed the plain-text part and the html/template layout from the same
// strings, so the two alternative parts never disagree.
func renderMail(locale string, brand Brand, cp mailCopy, code string, minutes int) (Message, error) {
	loc := NormalizeLocale(locale)
	render := func(frag string) (string, error) { return renderCopy(frag, brand.Name, minutes) }
	subject, err := render(cp.Subject)
	if err != nil {
		return Message{}, err
	}
	preview, err := render(cp.Preview)
	if err != nil {
		return Message{}, err
	}
	heading, err := render(cp.Heading)
	if err != nil {
		return Message{}, err
	}
	lead, err := render(cp.Lead)
	if err != nil {
		return Message{}, err
	}

	msg := Message{Subject: subject}

	// Plain text: lead, blank line, code line, blank line, validity (the
	// test mail stops at the lead). validity feeds both parts, so the two
	// alternatives never disagree.
	var validity string
	var lines []string
	if code != "" {
		label, err := render(cp.CodeLabel)
		if err != nil {
			return Message{}, err
		}
		sep := ": "
		if loc == LocaleZh {
			sep = "："
		}
		validity, err = render(cp.Validity)
		if err != nil {
			return Message{}, err
		}
		lines = []string{lead, "", label + sep + code, "", validity}
	} else {
		lines = []string{lead}
	}
	msg.Text = strings.Join(lines, "\n") + "\n"

	// HTML from the shared layout.
	data := layoutData{
		Lang:          "en",
		Title:         subject,
		Preheader:     preview,
		MSOTableOpen:  msoTableOpen,
		MSOTableClose: msoTableClose,
		Instance:      brand.Name,
		Heading:       heading,
		Lead:          lead,
		Code:          code,
	}
	if loc == LocaleZh {
		data.Lang = "zh-CN"
	}
	if code != "" {
		data.Validity = validity
	}
	if inline := inlineFromIcon(brand.Icon); inline != nil {
		data.IconSrc = iconURL
		msg.Inline = []Inline{*inline}
	}
	var html bytes.Buffer
	if err := layoutTmpl.Execute(&html, data); err != nil {
		return Message{}, fmt.Errorf("mail: layout: %w", err)
	}
	msg.HTML = html.String()
	return msg, nil
}

// CodeMail renders the verification mail for a locale.
func CodeMail(locale string, brand Brand, code string, minutes int) (Message, error) {
	cp, ok := codeCopy[NormalizeLocale(locale)]
	if !ok {
		return Message{}, fmt.Errorf("mail: no code copy for locale %q", locale)
	}
	return renderMail(locale, brand, cp, code, minutes)
}

// TestMail renders the admin test mail. It carries no code and no link.
func TestMail(locale string, brand Brand) (Message, error) {
	cp, ok := testCopy[NormalizeLocale(locale)]
	if !ok {
		return Message{}, fmt.Errorf("mail: no test copy for locale %q", locale)
	}
	return renderMail(locale, brand, cp, "", 0)
}
