package mail

import (
	"errors"
	"strings"
	"testing"

	"prohibitorum/pkg/configx"
)

func TestCodeMailZhContainsInstanceAndCode(t *testing.T) {
	msg, err := CodeMail("zh", "MyInstance", "123456", 15)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Subject, "MyInstance") {
		t.Errorf("zh subject missing instance: %q", msg.Subject)
	}
	if !strings.Contains(msg.Text, "123456") {
		t.Errorf("zh body missing code: %q", msg.Text)
	}
	if !strings.Contains(msg.Text, "15") {
		t.Errorf("zh body missing expiry minutes: %q", msg.Text)
	}
	if strings.Contains(msg.Text, "http") {
		t.Errorf("zh body must not contain a link: %q", msg.Text)
	}
}

func TestCodeMailLocaleFallback(t *testing.T) {
	for _, locale := range []string{"", "en", "ja", "ZH", "zh-CN"} {
		msg, err := CodeMail(locale, "Inst", "000000", 15)
		if err != nil {
			t.Fatalf("locale %q: %v", locale, err)
		}
		if msg.Subject == "" || msg.Text == "" {
			t.Fatalf("locale %q rendered empty mail", locale)
		}
	}
	if msg, _ := CodeMail("ja", "Inst", "0", 1); !strings.Contains(msg.Text, "expire") {
		t.Errorf("unrecognized locale should fall back to en, got %q", msg.Text)
	}
	if msg, _ := CodeMail("zh-Hans", "Inst", "0", 1); !strings.Contains(msg.Text, "验证码") {
		t.Errorf("zh-Hans should map to zh, got %q", msg.Text)
	}
}

func TestTestMail(t *testing.T) {
	for _, locale := range []string{"zh", "en"} {
		msg, err := TestMail(locale, "MyInstance")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.Subject, "MyInstance") || !strings.Contains(msg.Text, "MyInstance") {
			t.Errorf("test mail %q missing instance: %+v", locale, msg)
		}
		if strings.Contains(msg.Text, "http") {
			t.Errorf("test mail %q must not contain a link: %q", locale, msg.Text)
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
