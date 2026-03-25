// Package notifier sends Gmail email notifications for new Facebook events.
package notifier

import (
	"bytes"
	"fmt"
	"net/smtp"
	"strings"
	"text/template"

	"github.com/jlightheart24/eventable/event"
)

const (
	smtpHost = "smtp.gmail.com"
	smtpPort = "587"
)

const emailBody = `New Facebook Events Detected
============================
{{range $page, $events := .}}
{{ $page }}
----------------------------------------
{{- range $events}}
  Title:       {{.Title}}
  When:        {{formatTime .StartTime}}{{if not .EndTime.IsZero}} – {{formatTime .EndTime}}{{end}}
  Location:    {{if .Location}}{{.Location}}{{else}}Not specified{{end}}
  Description: {{if .Description}}{{truncate .Description 200}}{{else}}—{{end}}
  Link:        {{.Link}}
{{end}}
{{end}}`

var tmplFuncs = template.FuncMap{
	"formatTime": event.FormatTime,
	"truncate": func(s string, n int) string {
		s = strings.ReplaceAll(s, "\n", " ")
		if len(s) <= n {
			return s
		}
		return s[:n] + "…"
	},
	"not": func(b bool) bool { return !b },
}

var emailTmpl = template.Must(template.New("email").Funcs(tmplFuncs).Parse(emailBody))

// Config holds the credentials and addresses needed to send notifications.
type Config struct {
	GmailUser     string // sender Gmail address
	GmailPassword string // Gmail App Password
	NotifyEmail   string // recipient address
}

// group organises events by page name.
func group(events []event.Event) map[string][]event.Event {
	m := make(map[string][]event.Event)
	for _, e := range events {
		m[e.PageName] = append(m[e.PageName], e)
	}
	return m
}

// Send emails the list of new events to the configured recipient.
// It is a no-op when events is empty.
func Send(cfg Config, events []event.Event) error {
	if len(events) == 0 {
		return nil
	}

	var buf bytes.Buffer
	if err := emailTmpl.Execute(&buf, group(events)); err != nil {
		return fmt.Errorf("rendering email template: %w", err)
	}

	subject := fmt.Sprintf("Eventable: %d new event(s) detected", len(events))
	msg := buildMessage(cfg.GmailUser, cfg.NotifyEmail, subject, buf.String())

	auth := smtp.PlainAuth("", cfg.GmailUser, cfg.GmailPassword, smtpHost)
	if err := smtp.SendMail(smtpHost+":"+smtpPort, auth, cfg.GmailUser, []string{cfg.NotifyEmail}, []byte(msg)); err != nil {
		return fmt.Errorf("sending email: %w", err)
	}
	return nil
}

func buildMessage(from, to, subject, body string) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		from, to, subject, body,
	)
}
