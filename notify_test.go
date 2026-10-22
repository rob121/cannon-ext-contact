package main

import (
	"strings"
	"testing"

	"github.com/rob121/cannon/extension"
)

func TestSubmissionFieldsUseLabels(t *testing.T) {
	form := contactForm{
		Name:         "Support",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	data := submissionData(`{"name":"Jane","email":"jane@example.com","message":"Hello there"}`)
	fields := submissionFields(form, data)
	got := map[string]string{}
	var order []string
	for _, field := range fields {
		got[field.Label] = field.Value
		order = append(order, field.Label)
	}
	if strings.Join(order, ",") != "Name,Email,Message" {
		t.Fatalf("field order = %v", order)
	}
	if got["Name"] != "Jane" || got["Email"] != "jane@example.com" || got["Message"] != "Hello there" {
		t.Fatalf("fields = %#v", got)
	}
	if strings.Contains(submissionText(form.Name, fields), "{") {
		t.Fatal("plain text should not be raw JSON")
	}
}

func TestSubmissionSubjectReplacements(t *testing.T) {
	form := contactForm{
		Name:    "Support",
		Subject: `{{.name}} <{{.email}}> wrote in`,
	}
	data := map[string]any{"name": "Jane", "email": "jane@example.com", "message": "Hello"}
	got := submissionSubject(form, data)
	if got != "Jane <jane@example.com> wrote in" {
		t.Fatalf("subject = %q", got)
	}
	form.Subject = ""
	if got := submissionSubject(form, data); got != "New contact form submission: Support" {
		t.Fatalf("default subject = %q", got)
	}
	if msg := validateSubjectTemplate("{{.email"); msg == "" {
		t.Fatal("expected invalid subject template to be rejected")
	}
}

func TestSubmissionFieldsExpandJSONObject(t *testing.T) {
	form := contactForm{Name: "Support", SchemaJSON: `{"type":"object","properties":{"payload":{"type":"string","title":"Details"}}}`}
	data := submissionData(`{"payload":"{\"email\":\"jane@example.com\",\"message\":\"Hello\"}"}`)
	fields := submissionFields(form, data)
	if len(fields) != 1 {
		t.Fatalf("fields = %#v", fields)
	}
	if strings.Contains(fields[0].Value, "{") || !strings.Contains(fields[0].Value, "Email: jane@example.com") || !strings.Contains(fields[0].Value, "Message: Hello") {
		t.Fatalf("value = %q", fields[0].Value)
	}
}

func TestFirstSchemaEmailUsesSchemaOrder(t *testing.T) {
	schema := `{
	  "type": "object",
	  "properties": {
	    "name": {"type": "string", "title": "Name"},
	    "work_email": {"type": "string", "format": "email", "title": "Work Email"},
	    "home_email": {"type": "string", "format": "email", "title": "Home Email"}
	  }
	}`
	data := map[string]any{
		"name":       "Jane",
		"work_email": "work@example.com",
		"home_email": "home@example.com",
	}
	if got := firstSchemaEmail(schema, data); got != "work@example.com" {
		t.Fatalf("reply-to = %q", got)
	}
	form := contactForm{Name: "Support", SchemaJSON: schema}
	fields := submissionFields(form, data)
	if len(fields) != 3 || fields[1].Label != "Work Email" || fields[1].Value != "work@example.com" {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestSubmissionMessageUsesHTMLTemplateAndReplyTo(t *testing.T) {
	app := &contactApp{
		info:      extension.Info{Name: extensionName},
		templates: extension.NewTemplates(templateFiles, "templates"),
	}
	form := contactForm{
		Name:         "Support",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
		MailTo:       "desk@example.com",
	}
	entry := contactEntry{DataJSON: `{"name":"Jane","email":"jane@example.com","message":"Hello"}`}
	data := submissionData(entry.DataJSON)
	fields := submissionFields(form, data)
	html, err := app.templates.Execute("contact/submission.html", map[string]any{
		"Subject":  "New contact form submission: Support",
		"FormName": form.Name,
		"Fields":   fields,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Support", "Name", "Jane", "Email", "jane@example.com", "Message", "Hello"} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, entry.DataJSON) {
		t.Fatalf("html should not include raw JSON: %s", html)
	}
	body := string(submissionMIME("Cannon", "noreply@example.com", []string{"desk@example.com"}, "New contact form submission: Support", submissionText(form.Name, fields), html, firstSchemaEmail(form.SchemaJSON, data)))
	if !strings.Contains(body, "Reply-To: jane@example.com") {
		t.Fatalf("missing reply-to:\n%s", body)
	}
	if !strings.Contains(body, "Content-Type: text/html") {
		t.Fatalf("missing html part:\n%s", body)
	}
	if strings.Contains(body, "Reply-To: desk@example.com") {
		t.Fatal("reply-to should be the submitted email, not Mail To")
	}
}
