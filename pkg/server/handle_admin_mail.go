package server

import (
	"context"
	"fmt"
	"time"

	"prohibitorum/pkg/account/email"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/mail"
	"prohibitorum/pkg/weberr"
)

// ----- GET /admin/settings/mail ---------------------------------------------

type mailSettingsOut struct {
	Body contract.MailSettingsView
}

func (s *Server) handleGetMailSettings(ctx context.Context, _ *struct{}) (*mailSettingsOut, error) {
	smtp := s.config.SMTP
	view := contract.MailSettingsView{Configured: smtp.Host != ""}
	if view.Configured {
		view.Host = smtp.Host
		view.Port = smtp.Port
		view.Security = smtp.Security
		view.From = smtp.From
	}
	return &mailSettingsOut{Body: view}, nil
}

// ----- POST /admin/settings/mail/test -----------------------------------------

type sendTestMailIn struct {
	Body struct {
		To     string `json:"to"`
		Locale string `json:"locale,omitempty"`
	}
}

type sendTestMailOut struct {
	Body struct {
		Sent bool `json:"sent"`
	}
}

func (s *Server) handleSendTestMail(ctx context.Context, in *sendTestMailIn) (*sendTestMailOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil || sess.Account == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if _, err := email.ValidateAddress(in.Body.To); err != nil {
		return nil, authErrToHuma(authn.ErrEmailInvalid())
	}
	if !s.mailSender.Available() {
		return nil, authErrToHuma(authn.ErrEmailDeliveryUnavailable())
	}

	wait, ok := s.emailLimits.AllowTestMail(ctx, sess.Account.ID)
	if !ok {
		return nil, rateLimitedHuma(wait)
	}

	msg, err := mail.TestMail(in.Body.Locale, s.mailBrand(ctx))
	if err != nil {
		return nil, fmt.Errorf("handleSendTestMail: template: %w", err)
	}
	msg.To = in.Body.To
	if err := s.mailSender.Send(ctx, msg); err != nil {
		return nil, authErrToHuma(authn.ErrEmailDeliveryFailed())
	}

	acctID := sess.Account.ID
	audit.RecordOrLog(ctx, s.Audit, audit.Record{
		AccountID: &acctID,
		Factor:    audit.FactorEmail,
		Event:     audit.EventVerificationSent,
		Detail:    map[string]any{"address": in.Body.To, "reason": "test"},
	})
	out := &sendTestMailOut{}
	out.Body.Sent = true
	return out, nil
}

// mailBrand snapshots the effective branding for an outbound mail: the
// instance name and icon as branding resolves them (DB override → config →
// built-in), not the config file's values.
func (s *Server) mailBrand(ctx context.Context) mail.Brand {
	name := s.branding.InstanceName(ctx)
	icon, _, _ := s.branding.Icon(ctx)
	return mail.Brand{Name: name, Icon: icon}
}

// rateLimitedHuma builds the 429 public error carrying both the
// retryAfterSeconds detail and the header-hinting field for the response
// transformer.
func rateLimitedHuma(wait time.Duration) *weberr.PublicError {
	secs := int((wait + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	pe := authn.ErrRateLimited().PublicError()
	pe.Details = map[string]any{"retryAfterSeconds": secs}
	pe.RetryAfterSeconds = secs
	return pe
}
