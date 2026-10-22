package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/containrrr/shoutrrr"
	shoutmail "github.com/containrrr/shoutrrr/pkg/services/smtp"
)

type submissionField struct {
	Label string
	Value string
}

func (app *contactApp) notifySubmission(settings contactSettings, form contactForm, entry contactEntry) {
	if err := app.sendContactNotification(settings, form, entry); err != nil {
		log.Printf("contact: send submission for %q: %v", form.Name, err)
	}
}

func (app *contactApp) sendContactNotification(settings contactSettings, form contactForm, entry contactEntry) error {
	if strings.TrimSpace(settings.ShoutrrrURL) == "" {
		return fmt.Errorf("shoutrrr_url is not configured")
	}
	data := submissionData(entry.DataJSON)
	fields := submissionFields(form, data)
	subject := submissionSubject(form, data)
	text := submissionText(form.Name, fields)
	html, err := app.templates.Execute("contact/submission.html", map[string]any{
		"Subject":  subject,
		"FormName": form.Name,
		"Fields":   fields,
	})
	if err != nil {
		return err
	}
	replyTo := firstSchemaEmail(form.SchemaJSON, data)
	return deliverSubmission(settings.ShoutrrrURL, form.MailTo, subject, text, html, replyTo)
}

func validateSubjectTemplate(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return ""
	}
	if _, err := template.New("subject").Option("missingkey=zero").Parse(pattern); err != nil {
		return "Subject is not a valid template."
	}
	return ""
}

func submissionSubject(form contactForm, data map[string]any) string {
	fallback := "New contact form submission: " + strings.TrimSpace(form.Name)
	pattern := strings.TrimSpace(form.Subject)
	if pattern == "" {
		return fallback
	}
	tmpl, err := template.New("subject").Option("missingkey=zero").Parse(pattern)
	if err != nil {
		return fallback
	}
	vars := map[string]string{}
	for name, value := range data {
		vars[name] = strings.Join(strings.Fields(formatSubmissionValue(value)), " ")
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, vars); err != nil {
		return fallback
	}
	subject := headerSafe(buf.String())
	if subject == "" {
		return fallback
	}
	return subject
}

func submissionData(raw string) map[string]any {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

func submissionFields(form contactForm, data map[string]any) []submissionField {
	labels := map[string]string{}
	for _, field := range contactFields(form) {
		labels[field.Name] = field.Label
	}
	var ordered []string
	seen := map[string]bool{}
	if len(uiControls(form.UISchemaJSON)) > 0 {
		for _, field := range contactFields(form) {
			if seen[field.Name] {
				continue
			}
			ordered = append(ordered, field.Name)
			seen[field.Name] = true
		}
	}
	for _, prop := range orderedSchemaProps(form.SchemaJSON) {
		if seen[prop.Name] {
			continue
		}
		if _, ok := labels[prop.Name]; !ok {
			labels[prop.Name] = fieldLabel(prop.Name, prop.Prop.Title)
		}
		ordered = append(ordered, prop.Name)
		seen[prop.Name] = true
	}
	for name := range data {
		if seen[name] {
			continue
		}
		labels[name] = fieldLabel(name, "")
		ordered = append(ordered, name)
	}
	out := make([]submissionField, 0, len(ordered))
	for _, name := range ordered {
		value, ok := data[name]
		if !ok || value == nil {
			continue
		}
		text := formatSubmissionValue(value)
		if strings.TrimSpace(text) == "" {
			continue
		}
		label := labels[name]
		if strings.TrimSpace(label) == "" {
			label = name
		}
		out = append(out, submissionField{Label: label, Value: text})
	}
	return out
}

func submissionText(formName string, fields []submissionField) string {
	var b strings.Builder
	fmt.Fprintf(&b, "New contact form submission for %s\n", strings.TrimSpace(formName))
	for _, field := range fields {
		fmt.Fprintf(&b, "\n%s: %s", field.Label, field.Value)
	}
	return b.String()
}

func formatSubmissionValue(value any) string {
	switch v := value.(type) {
	case string:
		return expandJSONValue(v)
	case bool:
		if v {
			return "Yes"
		}
		return "No"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	case map[string]any:
		return flattenMap(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			text := formatSubmissionValue(item)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return expandJSONValue(fmt.Sprint(v))
	}
}

func expandJSONValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}
	var object map[string]any
	if json.Unmarshal([]byte(trimmed), &object) == nil && len(object) > 0 {
		return flattenMap(object)
	}
	var list []any
	if json.Unmarshal([]byte(trimmed), &list) == nil && len(list) > 0 {
		return formatSubmissionValue(list)
	}
	return value
}

func flattenMap(object map[string]any) string {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, 0, len(names))
	for _, name := range names {
		text := formatSubmissionValue(object[name])
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines = append(lines, fieldLabel(name, "")+": "+text)
	}
	return strings.Join(lines, "\n")
}

type namedSchemaProp struct {
	Name string
	Prop schemaProperty
}

func orderedSchemaProps(raw string) []namedSchemaProp {
	var envelope struct {
		Properties json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || len(envelope.Properties) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(envelope.Properties))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil
	}
	var out []namedSchemaProp
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		name, ok := tok.(string)
		if !ok {
			break
		}
		var prop schemaProperty
		if err := dec.Decode(&prop); err != nil {
			break
		}
		out = append(out, namedSchemaProp{Name: name, Prop: prop})
	}
	return out
}

func firstSchemaEmail(schemaJSON string, data map[string]any) string {
	for _, prop := range orderedSchemaProps(schemaJSON) {
		if !strings.EqualFold(strings.TrimSpace(prop.Prop.Format), "email") {
			continue
		}
		value, _ := data[prop.Name].(string)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		addr, err := mail.ParseAddress(value)
		if err != nil || strings.TrimSpace(addr.Address) == "" {
			continue
		}
		return addr.Address
	}
	return ""
}

func deliverSubmission(rawURL, mailTo, subject, text, html, replyTo string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return err
	}
	if !strings.EqualFold(parsed.Scheme, shoutmail.Scheme) {
		return shoutrrr.Send(rawURL, text)
	}
	cfg := &shoutmail.Config{Port: 25, UseStartTLS: true}
	if err := cfg.SetURL(parsed); err != nil {
		return err
	}
	cfg.FixEmailTags()
	recipients := append([]string{}, cfg.ToAddresses...)
	if extra := strings.TrimSpace(mailTo); extra != "" {
		recipients = append(recipients, extra)
	}
	recipients = uniqueEmails(recipients)
	if len(recipients) == 0 {
		return fmt.Errorf("submission recipient is required")
	}
	body := submissionMIME(cfg.FromName, cfg.FromAddress, recipients, subject, text, html, replyTo)
	return sendSMTP(cfg, recipients, body)
}

func uniqueEmails(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func submissionMIME(fromName, fromEmail string, to []string, subject, text, html, replyTo string) []byte {
	from := strings.TrimSpace(fromEmail)
	if name := headerSafe(fromName); name != "" {
		from = name + " <" + from + ">"
	}
	lines := []string{
		"From: " + from,
		"To: " + strings.Join(to, ", "),
		"Subject: " + headerSafe(subject),
	}
	if replyTo = headerSafe(replyTo); replyTo != "" {
		lines = append(lines, "Reply-To: "+replyTo)
	}
	boundary := "cannon-contact-boundary"
	lines = append(lines,
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary="+boundary,
		"",
		"--"+boundary,
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		text,
		"--"+boundary,
		"Content-Type: text/html; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		html,
		"--"+boundary+"--",
		"",
	)
	return []byte(strings.Join(lines, "\r\n"))
}

func headerSafe(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}

const smtpDialTimeout = 10 * time.Second

func sendSMTP(cfg *shoutmail.Config, recipients []string, body []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	var auth smtp.Auth
	if strings.TrimSpace(cfg.Username) != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	if cfg.Port == 465 {
		return sendSMTPTLS(dialer, addr, cfg.Host, auth, cfg.FromAddress, recipients, body)
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(smtpDialTimeout))
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if cfg.UseStartTLS {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	return writeSMTP(client, cfg.FromAddress, recipients, body)
}

func sendSMTPTLS(dialer *net.Dialer, addr, host string, auth smtp.Auth, from string, to []string, body []byte) error {
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(smtpDialTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	return writeSMTP(client, from, to, body)
}

func writeSMTP(client *smtp.Client, from string, to []string, body []byte) error {
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
