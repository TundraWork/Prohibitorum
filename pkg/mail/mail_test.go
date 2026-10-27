package mail

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"regexp"
	"strings"
	"testing"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/imageutil"
)

var (
	srcRe     = regexp.MustCompile(`src="[^"]*"`)
	headRe    = regexp.MustCompile(`(?s)<head>.*</head>`)
	tagRe     = regexp.MustCompile(`<[^>]*>`)
	preheadRe = regexp.MustCompile(`<div style="display:none;[^"]*">([^<]*)</div>`)
	// Mail clients autolink bare hosts such as example.com or www.example.
	bareHostRe = regexp.MustCompile(`(?i)\bwww\.|\b[a-z0-9-]+\.[a-z]{2,}\b`)
)

// visibleText drops the head, the tags and the attributes, leaving the text
// a mail client could autolink.
func visibleText(html string) string {
	return tagRe.ReplaceAllString(headRe.ReplaceAllString(html, ""), " ")
}

func brand(name string, icon []byte) Brand { return Brand{Name: name, Icon: icon} }

// pngIcon encodes a tiny PNG so icon embedding can be exercised without
// fixtures.
func pngIcon(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// webpIcon produces a real WebP through the same pipeline branding uses for
// stored icons.
func webpIcon(t *testing.T) []byte {
	t.Helper()
	out, _, err := imageutil.ProcessSquareWebP(pngIcon(t), imageutil.IconSize, true)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// assertNoLinks fails when the part carries anything link-like: both mail
// bodies stay link-free by design (PHB-158), so no <a>, no href, no URL
// scheme, and src= only ever naming the inline icon CID.
func assertNoLinks(t *testing.T, part, html string) {
	t.Helper()
	for _, bad := range []string{"<a ", "<a>", "href", "<script", "<form", "http://", "https://"} {
		if strings.Contains(part, bad) {
			t.Errorf("mail contains %q: %.120s", bad, part)
		}
	}
	text := part
	if html != "" {
		text = visibleText(html)
	}
	if m := bareHostRe.FindString(text); m != "" {
		t.Errorf("mail text contains autolinkable host %q", m)
	}
	if html == "" {
		return
	}
	for _, m := range srcRe.FindAllString(html, -1) {
		if m != `src="cid:instance-icon"` {
			t.Errorf("unexpected src %q in HTML", m)
		}
	}
}

func TestCodeMailPartsZhEn(t *testing.T) {
	icon := pngIcon(t)
	for _, locale := range []string{"zh", "en"} {
		msg, err := CodeMail(locale, brand("MyInstance", icon), "123456", 15)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Subject, "MyInstance") {
			t.Errorf("%s subject missing instance: %q", locale, msg.Subject)
		}
		// The code never leaks into subject or preview text.
		if strings.Contains(msg.Subject, "123456") {
			t.Errorf("%s subject leaks code: %q", locale, msg.Subject)
		}
		pre := preheadRe.FindStringSubmatch(msg.HTML)
		if pre == nil || pre[1] == "" {
			t.Errorf("%s html has no preview text", locale)
		} else if strings.Contains(pre[1], "123456") {
			t.Errorf("%s preview text leaks code: %q", locale, pre[1])
		}
		assertNoLinks(t, msg.Text, "")
		assertNoLinks(t, msg.HTML, msg.HTML)
		if strings.Contains(msg.HTML, "123456") && !strings.Contains(msg.HTML, ">123456<") {
			t.Errorf("%s code not a bare text node", locale)
		}
		for _, want := range []string{"123456", "MyInstance", "15"} {
			if !strings.Contains(msg.Text, want) {
				t.Errorf("%s text missing %q: %q", locale, want, msg.Text)
			}
			if !strings.Contains(msg.HTML, want) {
				t.Errorf("%s html missing %q", locale, want)
			}
		}
		wantLang := `<html lang="zh-CN">`
		if locale == "en" {
			wantLang = `<html lang="en">`
		}
		if !strings.Contains(msg.HTML, wantLang) {
			t.Errorf("%s html lang missing: %q", locale, wantLang)
		}
		for _, want := range []string{"color-scheme", "prefers-color-scheme: dark", "<!--[if mso]>"} {
			if !strings.Contains(msg.HTML, want) {
				t.Errorf("%s html missing %q", locale, want)
			}
		}
		if strings.Contains(msg.HTML, "ZgotmplZ") {
			t.Errorf("%s html has ZgotmplZ (URL filter hit a constant)", locale)
		}
		if len(msg.Inline) != 1 || msg.Inline[0].ContentID != "instance-icon" {
			t.Errorf("%s inline = %+v", locale, msg.Inline)
		}
	}
}

func TestTestMailPartsZhEn(t *testing.T) {
	for _, locale := range []string{"zh", "en"} {
		msg, err := TestMail(locale, brand("MyInstance", nil))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Subject, "MyInstance") || !strings.Contains(msg.Text, "MyInstance") {
			t.Errorf("test mail %q missing instance: %+v", locale, msg)
		}
		assertNoLinks(t, msg.Text, "")
		assertNoLinks(t, msg.HTML, msg.HTML)
		if len(msg.Inline) != 0 {
			t.Errorf("test mail without icon embeds %+v", msg.Inline)
		}
		if strings.Contains(msg.HTML, "<img") {
			t.Errorf("test mail without icon renders <img>")
		}
	}
}

func TestMailIconTypes(t *testing.T) {
	cases := []struct {
		name  string
		icon  []byte
		ctype string
	}{
		{"webp", webpIcon(t), "image/webp"},
		{"png", pngIcon(t), "image/png"},
	}
	for _, tc := range cases {
		msg, err := CodeMail("en", brand("Inst", tc.icon), "000000", 15)
		if err != nil {
			t.Fatal(err)
		}
		if len(msg.Inline) != 1 {
			t.Fatalf("%s: inline = %+v", tc.name, msg.Inline)
		}
		if msg.Inline[0].ContentType != tc.ctype {
			t.Errorf("%s: content type = %q, want %q", tc.name, msg.Inline[0].ContentType, tc.ctype)
		}
		if !strings.Contains(msg.HTML, `src="cid:instance-icon"`) {
			t.Errorf("%s: html missing cid img", tc.name)
		}
	}

	// Unknown bytes: no embed, mail still renders.
	msg, err := CodeMail("en", brand("Inst", []byte("not an image")), "000000", 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Inline) != 0 || strings.Contains(msg.HTML, "<img") {
		t.Errorf("unknown icon type embeds %+v", msg.Inline)
	}
}

func TestMailIconSizeCap(t *testing.T) {
	big := append(pngIcon(t), make([]byte, maxInlineIconBytes)...)
	msg, err := CodeMail("en", brand("Inst", big), "000000", 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Inline) != 0 || strings.Contains(msg.HTML, "<img") {
		t.Errorf("icon over the cap embedded (%d bytes)", len(big))
	}
}

func TestMailInstanceNameSingleLine(t *testing.T) {
	msg, err := CodeMail("zh", brand("A\r\nBcc: x@example.test\t\x00B", nil), "123456", 15)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(msg.Subject, "\r\n\t\x00") {
		t.Errorf("subject keeps control characters: %q", msg.Subject)
	}
	if !strings.HasPrefix(msg.Subject, "A Bcc: x@example.test B ") {
		t.Errorf("subject = %q, want the name folded onto one line", msg.Subject)
	}
	if first, _, _ := strings.Cut(msg.Text, "\n"); !strings.Contains(first, "Bcc") {
		t.Errorf("plain text splits the instance name across lines: %q", msg.Text)
	}
}

func TestMailEscapesInstanceName(t *testing.T) {
	msg, err := CodeMail("en", brand("A&B <x>", nil), "123456", 15)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(msg.HTML, "A&B <x>") {
		t.Errorf("instance name unescaped in HTML")
	}
	if !strings.Contains(msg.HTML, "A&amp;B") {
		t.Errorf("instance name not escaped: %.80s", msg.HTML)
	}
}

func TestCodeMailLocaleFallback(t *testing.T) {
	for _, locale := range []string{"", "en", "ja", "ZH", "zh-CN"} {
		msg, err := CodeMail(locale, brand("Inst", nil), "000000", 15)
		if err != nil {
			t.Fatalf("locale %q: %v", locale, err)
		}
		if msg.Subject == "" || msg.Text == "" || msg.HTML == "" {
			t.Fatalf("locale %q rendered empty mail", locale)
		}
	}
	if msg, _ := CodeMail("ja", brand("Inst", nil), "0", 1); !strings.Contains(msg.Text, "Valid for") {
		t.Errorf("unrecognized locale should fall back to en, got %q", msg.Text)
	}
	if msg, _ := CodeMail("zh-Hans", brand("Inst", nil), "0", 1); !strings.Contains(msg.Text, "验证码") {
		t.Errorf("zh-Hans should map to zh, got %q", msg.Text)
	}
}

func TestMailCopyComplete(t *testing.T) {
	for locale, cp := range codeCopy {
		if cp.Subject == "" || cp.Preview == "" || cp.Heading == "" || cp.Lead == "" || cp.CodeLabel == "" || cp.Validity == "" {
			t.Errorf("codeCopy[%s] has an empty field: %+v", locale, cp)
		}
	}
	for locale, cp := range testCopy {
		if cp.Subject == "" || cp.Preview == "" || cp.Heading == "" || cp.Lead == "" {
			t.Errorf("testCopy[%s] has an empty field: %+v", locale, cp)
		}
	}
}

func TestNullSender(t *testing.T) {
	s := NewSMTPSender(configx.SMTPConfig{})
	if s.Available() {
		t.Error("null sender reports available")
	}
	err := s.Send(t.Context(), Message{To: "x@example.com"})
	if !errors.Is(err, ErrDeliveryUnavailable) {
		t.Errorf("null sender error = %v, want ErrDeliveryUnavailable", err)
	}
}

func TestSMTPSenderAvailable(t *testing.T) {
	s := NewSMTPSender(configx.SMTPConfig{Host: "smtp.example.com", Port: 587, Security: "starttls", From: "noreply@example.com"})
	if !s.Available() {
		t.Error("smtp sender reports unavailable")
	}
}
