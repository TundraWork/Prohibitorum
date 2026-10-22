// Package mail sends the instance's outbound email: address-verification
// codes and the admin test mail. Delivery is synchronous within the request
// and bounded by the configured timeout; nothing queues or retries.
//
// An empty-host configuration yields NullSender, which fails every send with
// ErrDeliveryUnavailable so callers can surface "mail is not configured"
// without special-casing.
package mail

import (
	"context"
	"errors"
	"fmt"

	gomail "github.com/wneessen/go-mail"

	"prohibitorum/pkg/configx"
)

// ErrDeliveryUnavailable is returned by a Sender built from an empty SMTP
// host: the instance has no mail transport, and the caller should say so
// rather than report a send failure.
var ErrDeliveryUnavailable = errors.New("mail: delivery unavailable (smtp.host is empty)")

// Message is one outbound email. To is a bare address; Subject and Text are
// pre-rendered by the caller (see templates.go).
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers messages within the caller's context/timeout.
type Sender interface {
	Send(ctx context.Context, msg Message) error
	// Available reports whether the instance has a mail transport at all.
	Available() bool
}

// NewSMTPSender builds the Sender for cfg: a real SMTP client when host is
// set, NullSender otherwise.
func NewSMTPSender(cfg configx.SMTPConfig) Sender {
	if cfg.Host == "" {
		return NullSender{}
	}
	return &smtpSender{cfg: cfg}
}

// NullSender is the empty-host implementation.
type NullSender struct{}

func (NullSender) Send(context.Context, Message) error { return ErrDeliveryUnavailable }
func (NullSender) Available() bool                     { return false }

type smtpSender struct {
	cfg configx.SMTPConfig
}

func (s *smtpSender) Available() bool { return true }

func (s *smtpSender) Send(ctx context.Context, msg Message) error {
	opts := []gomail.Option{gomail.WithTimeout(s.cfg.Timeout)}
	switch s.cfg.Security {
	case "tls":
		// Implicit TLS from the first byte (usually port 465).
		opts = append(opts, gomail.WithPort(s.cfg.Port), gomail.WithSSLPort(false))
	case "none":
		// Plaintext to a trusted relay; NoTLS forces the plain port.
		opts = append(opts, gomail.WithPort(s.cfg.Port), gomail.WithTLSPolicy(gomail.NoTLS))
	default:
		// "starttls" — mandatory STARTTLS upgrade on the plain port.
		opts = append(opts, gomail.WithPort(s.cfg.Port), gomail.WithTLSPolicy(gomail.TLSMandatory))
	}

	client, err := gomail.NewClient(s.cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("mail: client: %w", err)
	}
	defer client.Close() //nolint:errcheck // best-effort; the transaction is done

	if s.cfg.Username != "" {
		client.SetUsername(s.cfg.Username)
		client.SetPassword(s.cfg.Password)
		client.SetSMTPAuth(gomail.SMTPAuthLogin)
	}

	m := gomail.NewMsg()
	if err := m.From(s.cfg.From); err != nil {
		return fmt.Errorf("mail: from %q: %w", s.cfg.From, err)
	}
	if err := m.To(msg.To); err != nil {
		return fmt.Errorf("mail: to %q: %w", msg.To, err)
	}
	m.Subject(msg.Subject)
	m.SetBodyString(gomail.TypeTextPlain, msg.Text)

	if err := client.DialAndSendWithContext(ctx, m); err != nil {
		return fmt.Errorf("mail: send: %w", err)
	}
	return nil
}
