package mail

import (
	"fmt"
	"strings"
	"text/template"
)

// Locale selects the template pair. Anything unrecognized falls back to en,
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

// codeMailData fills the verification-code templates.
type codeMailData struct {
	Instance string
	Code     string
	Minutes  int
}

var codeTemplates = map[Locale]*codeTemplatePair{
	LocaleZh: mustParseCode(LocaleZh),
	LocaleEn: mustParseCode(LocaleEn),
}

type codeTemplatePair struct {
	subject *template.Template
	body    *template.Template
}

func mustParseCode(l Locale) *codeTemplatePair {
	return &codeTemplatePair{
		subject: template.Must(template.New("subject").Parse(codeSubjects[l])),
		body:    template.Must(template.New("body").Parse(codeBodies[l])),
	}
}

var codeSubjects = map[Locale]string{
	LocaleZh: "{{.Instance}} 邮箱验证码",
	LocaleEn: "{{.Instance}} email verification code",
}

// Bodies are plain text with no links by design: the code is only ever typed
// back into the dashboard by the person who owns the mailbox, and a link
// would make the mail itself a phishing surface.
var codeBodies = map[Locale]string{
	LocaleZh: `你正在 {{.Instance}} 验证一个邮箱地址。

验证码：{{.Code}}

{{.Minutes}} 分钟内有效。如果不是你本人操作，可以忽略这封邮件。
`,
	LocaleEn: `You are verifying an email address on {{.Instance}}.

Verification code: {{.Code}}

It expires in {{.Minutes}} minutes. If this wasn't you, you can ignore this email.
`,
}

// CodeMail renders the verification mail for a locale.
func CodeMail(locale string, instance, code string, minutes int) (Message, error) {
	pair, ok := codeTemplates[NormalizeLocale(locale)]
	if !ok {
		return Message{}, fmt.Errorf("mail: no template for locale %q", locale)
	}
	data := codeMailData{Instance: instance, Code: code, Minutes: minutes}
	var subject, body strings.Builder
	if err := pair.subject.Execute(&subject, data); err != nil {
		return Message{}, fmt.Errorf("mail: render subject: %w", err)
	}
	if err := pair.body.Execute(&body, data); err != nil {
		return Message{}, fmt.Errorf("mail: render body: %w", err)
	}
	return Message{Subject: subject.String(), Text: body.String()}, nil
}

// TestMail renders the admin test mail. It carries no code and no link.
func TestMail(locale string, instance string) (Message, error) {
	pair, ok := testMails[NormalizeLocale(locale)]
	if !ok {
		return Message{}, fmt.Errorf("mail: no test template for locale %q", locale)
	}
	data := struct{ Instance string }{Instance: instance}
	var subject, body strings.Builder
	if err := pair[0].Execute(&subject, data); err != nil {
		return Message{}, fmt.Errorf("mail: render test subject: %w", err)
	}
	if err := pair[1].Execute(&body, data); err != nil {
		return Message{}, fmt.Errorf("mail: render test body: %w", err)
	}
	return Message{Subject: subject.String(), Text: body.String()}, nil
}

var testMails = map[Locale][2]*template.Template{
	LocaleZh: {
		template.Must(template.New("zh-subject").Parse("{{.Instance}} 测试邮件")),
		template.Must(template.New("zh-body").Parse("这是 {{.Instance}} 的一封测试邮件。收到它说明本实例的发信配置是正常的。\n")),
	},
	LocaleEn: {
		template.Must(template.New("en-subject").Parse("{{.Instance}} test mail")),
		template.Must(template.New("en-body").Parse("This is a test mail from {{.Instance}}. Receiving it means this instance's outbound mail works.\n")),
	},
}
