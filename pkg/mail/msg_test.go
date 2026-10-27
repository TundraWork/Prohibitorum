package mail

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
)

// readPart fully decodes one MIME part: quoted-printable and base64 are the
// transfer encodings go-mail emits.
func readPart(t *testing.T, header textproto.MIMEHeader, body io.Reader) ([]byte, error) {
	t.Helper()
	enc := header.Get("Content-Transfer-Encoding")
	raw, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(enc) {
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(raw)))
	case "base64":
		dec := base64.NewDecoder(base64.StdEncoding, bytes.NewReader(raw))
		return io.ReadAll(dec)
	default:
		return raw, nil
	}
}

// TestBuildMsgMIMEStructure pins the wire shape of a full mail: related
// wrapping the alternative (plain first, HTML second) and the icon part,
// matching what plan.md §MIME structure specifies.
func TestBuildMsgMIMEStructure(t *testing.T) {
	msg, err := CodeMail("zh", brand("示例实例", pngIcon(t)), "123456", 15)
	if err != nil {
		t.Fatal(err)
	}
	msg.To = "dest@example.test"
	m, err := buildMsg("noreply@example.test", msg)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(buf.String(), "\r\n") {
		if len(line) > 78 {
			t.Errorf("line %d is %d octets: %.60q", i+1, len(line), line)
		}
	}

	parsed, err := mail.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/related" {
		t.Fatalf("top level = %q, want multipart/related", mediaType)
	}
	related := multipart.NewReader(parsed.Body, params["boundary"])
	parts := 0
	for {
		p, err := related.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		parts++
		if parts == 1 {
			if ct := p.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/alternative") {
				t.Fatalf("first related part = %q, want multipart/alternative", ct)
			}
			mt, ps, err := mime.ParseMediaType(p.Header.Get("Content-Type"))
			if err != nil {
				t.Fatal(err)
			}
			if mt != "multipart/alternative" {
				t.Fatalf("first related part = %q", mt)
			}
			alt := multipart.NewReader(p, ps["boundary"])
			n := 0
			for {
				ap, err := alt.NextRawPart() // raw: NextPart strips a quoted-printable header
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				n++
				content, err := readPart(t, ap.Header, ap)
				if err != nil {
					t.Fatal(err)
				}
				ct := ap.Header.Get("Content-Type")
				if cte := ap.Header.Get("Content-Transfer-Encoding"); cte != "quoted-printable" && cte != "base64" {
					t.Errorf("alternative part %d transfer encoding = %q", n, cte)
				}
				switch n {
				case 1:
					if !strings.HasPrefix(ct, "text/plain") {
						t.Errorf("alternative part 1 = %q, want text/plain", ct)
					}
					if !strings.Contains(string(content), "123456") || !strings.Contains(string(content), "示例实例") {
						t.Errorf("plain part missing code/instance: %q", content)
					}
				case 2:
					if !strings.HasPrefix(ct, "text/html") {
						t.Errorf("alternative part 2 = %q, want text/html", ct)
					}
					if !strings.Contains(string(content), "123456") || !strings.Contains(string(content), "示例实例") {
						t.Errorf("html part missing code/instance: %q", content)
					}
				default:
					t.Errorf("unexpected alternative part %d (%q)", n, ct)
				}
			}
			if n != 2 {
				t.Errorf("alternative parts = %d, want 2", n)
			}
			continue
		}
		// The icon part: inline disposition, CID, detected type.
		if got := p.Header.Get("Content-ID"); got != "<instance-icon>" {
			t.Errorf("icon Content-ID = %q, want <instance-icon>", got)
		}
		if disp, dp, err := mime.ParseMediaType(p.Header.Get("Content-Disposition")); err != nil || disp != "inline" || dp["filename"] != "icon.png" {
			t.Errorf("icon Content-Disposition = %q", p.Header.Get("Content-Disposition"))
		}
		if cte := p.Header.Get("Content-Transfer-Encoding"); cte != "base64" {
			t.Errorf("icon transfer encoding = %q, want base64", cte)
		}
		if ct := p.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
			t.Errorf("icon Content-Type = %q, want image/png", ct)
		}
		data, err := readPart(t, p.Header, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) == 0 {
			t.Error("icon part is empty")
		}
	}
	if parts != 2 {
		t.Errorf("related parts = %d, want 2 (alternative + icon)", parts)
	}
}

// TestBuildMsgSubjectHeaderInjection writes a raw subject carrying CRLF
// straight to buildMsg: the header must stay one encoded field.
func TestBuildMsgSubjectHeaderInjection(t *testing.T) {
	m, err := buildMsg("noreply@example.test", Message{
		To:      "x@example.test",
		Subject: "名称\r\nBcc: evil@example.test",
		Text:    "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Header.Get("Bcc"); got != "" {
		t.Errorf("subject injected a Bcc header: %q", got)
	}
	raw := parsed.Header.Get("Subject")
	if !strings.HasPrefix(raw, "=?UTF-8?") {
		t.Errorf("subject not RFC 2047 encoded: %q", raw)
	}
	subj, err := new(mime.WordDecoder).DecodeHeader(raw)
	if err != nil || !strings.HasPrefix(subj, "名称") {
		t.Errorf("subject = %q (%v)", subj, err)
	}
}

func TestBuildMsgAlternativeWithoutIcon(t *testing.T) {
	msg, err := TestMail("en", brand("Inst", nil))
	if err != nil {
		t.Fatal(err)
	}
	msg.To = "dest@example.test"
	m, err := buildMsg("noreply@example.test", msg)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, params, err := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if err != nil {
		t.Fatal(err)
	}
	if mediaType != "multipart/alternative" {
		t.Fatalf("top level = %q, want multipart/alternative", mediaType)
	}
	alt := multipart.NewReader(parsed.Body, params["boundary"])
	n := 0
	for {
		p, err := alt.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
		content, err := readPart(t, p.Header, p)
		if err != nil {
			t.Fatal(err)
		}
		want := "text/plain"
		if n == 2 {
			want = "text/html"
		}
		if ct := p.Header.Get("Content-Type"); !strings.HasPrefix(ct, want) {
			t.Errorf("part %d = %q, want %q", n, ct, want)
		}
		if !strings.Contains(string(content), "Inst") {
			t.Errorf("part %d missing instance", n)
		}
	}
	if n != 2 {
		t.Errorf("parts = %d, want 2", n)
	}
}

func TestBuildMsgTextOnly(t *testing.T) {
	m, err := buildMsg("noreply@example.test", Message{To: "x@example.test", Subject: "s", Text: "body"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := m.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	if ct := parsed.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	body, err := readPart(t, textproto.MIMEHeader(parsed.Header), parsed.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "body") {
		t.Errorf("text body = %q", body)
	}
}
