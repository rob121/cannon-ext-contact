package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rob121/cannon/extension"
)

func TestCapabilities(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var got struct {
		Capabilities map[string]string `json:"capabilities"`
		Defaults     struct {
			Admin struct {
				MenuName string `json:"menu_name"`
			} `json:"admin"`
		} `json:"defaults"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got.Capabilities["page"] != "/page" {
		t.Fatalf("expected page capability /page, got %q", got.Capabilities["page"])
	}
	if got.Capabilities["admin"] != "/admin" {
		t.Fatalf("expected admin capability /admin, got %q", got.Capabilities["admin"])
	}
	if got.Capabilities["help"] != "/help" {
		t.Fatalf("expected help capability /help, got %q", got.Capabilities["help"])
	}
	if got.Capabilities["configuration"] != "/configuration" {
		t.Fatalf("expected configuration capability /configuration, got %q", got.Capabilities["configuration"])
	}
	if got.Capabilities["block"] != "/block" {
		t.Fatalf("expected block capability /block, got %q", got.Capabilities["block"])
	}
	if got.Capabilities["endpoint"] != "/endpoint" {
		t.Fatalf("expected endpoint capability /endpoint, got %q", got.Capabilities["endpoint"])
	}
	if got.Capabilities["data"] != "/data" {
		t.Fatalf("expected data capability /data, got %q", got.Capabilities["data"])
	}
	if got.Capabilities["templates"] != "/templates" {
		t.Fatalf("expected templates capability /templates, got %q", got.Capabilities["templates"])
	}
	if _, ok := got.Capabilities["meta"]; ok {
		t.Fatal("meta is a built-in endpoint and should not be listed as a capability")
	}
	if _, ok := got.Capabilities["hooks"]; ok {
		t.Fatal("hooks should not be listed unless the extension registers hook subscriptions")
	}
	if got.Defaults.Admin.MenuName != defaultAdminMenu {
		t.Fatalf("expected admin menu default %q, got %q", defaultAdminMenu, got.Defaults.Admin.MenuName)
	}
}

func TestTemplatesListOverridablePaths(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/templates", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var got extension.TemplateListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := map[string]string{
		"contact/admin_form.html": "extension/contact/admin_form.html",
		"contact/admin_list.html": "extension/contact/admin_list.html",
		"contact/block.html":      "extension/contact/block.html",
		"contact/form.html":       "extension/contact/form.html",
	}
	if len(got.Templates) != len(want) {
		t.Fatalf("expected %d templates, got %#v", len(want), got.Templates)
	}
	for _, tmpl := range got.Templates {
		if tmpl.OverridePath != want[tmpl.Path] {
			t.Fatalf("template %q override path: got %q want %q", tmpl.Path, tmpl.OverridePath, want[tmpl.Path])
		}
	}
}

func TestMeta(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/meta", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var got struct {
		Name          string `json:"name"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		Version       string `json:"version"`
		UpdateURLBase string `json:"update_url_base"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != extensionName {
		t.Fatalf("expected extension name %q, got %q", extensionName, got.Name)
	}
	if got.Title != extensionTitle {
		t.Fatalf("expected extension title %q, got %q", extensionTitle, got.Title)
	}
	if got.Description != extensionDescription {
		t.Fatalf("expected extension description %q, got %q", extensionDescription, got.Description)
	}
	if got.Version != extensionVersion {
		t.Fatalf("expected extension version %q, got %q", extensionVersion, got.Version)
	}
	if got.UpdateURLBase != updateURLBase {
		t.Fatalf("expected update URL base %q, got %q", updateURLBase, got.UpdateURLBase)
	}
}

func TestInstallCreatesContactTables(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()

	resp := postWire(t, server, "/install", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/install",
		SiteID: "example",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected install status %d, got %d: %s", http.StatusOK, resp.StatusCode, resp.Body)
	}

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"contact_settings", "contact_forms", "contact_entries"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("expected table %s to exist", table)
		}
	}
}

func TestConfigurationSupportsSingleShoutrrrURL(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	req := httptest.NewRequest(http.MethodGet, "/configuration", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var doc extension.ConfigurationDocument
	if err := json.NewDecoder(rec.Body).Decode(&doc); err != nil {
		t.Fatalf("decode configuration: %v", err)
	}
	if len(doc.Sections) != 1 {
		t.Fatalf("expected one configuration section, got %d", len(doc.Sections))
	}
	if !bytes.Contains(doc.Sections[0].Schema, []byte(`"shoutrrr_url"`)) {
		t.Fatalf("expected shoutrrr_url schema, got %s", string(doc.Sections[0].Schema))
	}

	saveBody := bytes.NewReader([]byte(`{"section":"general","data":{"shoutrrr_url":"logger://"}}`))
	req = httptest.NewRequest(http.MethodPost, "/configuration", saveBody)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}

	settings, err := loadContactSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.ShoutrrrURL != "logger://" {
		t.Fatalf("expected saved shoutrrr url, got %q", settings.ShoutrrrURL)
	}
}

func TestPageListIncludesActiveFormOptions(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	active := contactForm{
		Name:         "General Contact",
		MailTo:       "hello@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	inactive := contactForm{
		Name:         "Old Contact",
		MailTo:       "old@example.com",
		Status:       "inactive",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&inactive).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var got extension.PageListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode page list: %v", err)
	}
	if len(got.Pages) != 1 {
		t.Fatalf("expected one page definition, got %#v", got.Pages)
	}
	page := got.Pages[0]
	if page.ID != "contact-form" || page.Title != "Contact Form" {
		t.Fatalf("unexpected page definition: %#v", page)
	}
	if len(page.Fields) != 1 || page.Fields[0].Name != "form_id" || page.Fields[0].Type != "select" {
		t.Fatalf("unexpected page fields: %#v", page.Fields)
	}
	if len(page.Fields[0].Options) != 1 {
		t.Fatalf("expected one active form option, got %#v", page.Fields[0].Options)
	}
	if page.Fields[0].Options[0].Value != fmt.Sprint(active.ID) || page.Fields[0].Options[0].Label != "General Contact" {
		t.Fatalf("unexpected page form option: %#v", page.Fields[0].Options[0])
	}
}

func TestContactFormPageRendersSelectedActiveForm(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:         "Support Request",
		ShowName:     true,
		Introduction: "Tell us what you need.",
		MailTo:       "support@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	resp := postWire(t, server, "/page/contact-form", extension.WireRequest{
		Method:   http.MethodGet,
		URL:      "/contact",
		CSRF:     "page-token",
		SiteID:   "example",
		PageItem: "contact-form",
		PageData: map[string]any{
			"form_id": float64(form.ID),
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected wire status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	actionURL := extension.PublicDataURL(extensionName, "example", "contact/submit")
	for _, want := range []string{"Support Request", "Tell us what you need.", `name="email" type="email"`, `name="message"`, "<textarea", `action="` + actionURL + `"`, `name="return_url" value="/contact"`, `name="_csrf" value="page-token"`, `<captcha context="form" provider="any"></captcha>`} {
		if !strings.Contains(resp.Body, want) {
			t.Fatalf("expected page body to contain %q, got %q", want, resp.Body)
		}
	}
}

func TestContactFormPageHidesNameWhenDisabled(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:         "Hidden Title Form",
		ShowName:     false,
		Introduction: "Still show intro.",
		MailTo:       "support@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	resp := postWire(t, server, "/page/contact-form", extension.WireRequest{
		Method:   http.MethodGet,
		URL:      "/contact",
		SiteID:   "example",
		PageItem: "contact-form",
		PageData: map[string]any{"form_id": float64(form.ID)},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected wire status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if strings.Contains(resp.Body, "Hidden Title Form") {
		t.Fatalf("expected form name to be hidden, got %q", resp.Body)
	}
	if !strings.Contains(resp.Body, "Still show intro.") {
		t.Fatalf("expected introduction to remain visible, got %q", resp.Body)
	}
}

func TestEndpointList(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/endpoint", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var got extension.EndpointListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode endpoint list: %v", err)
	}
	if len(got.Endpoints) != 1 {
		t.Fatalf("expected one endpoint definition, got %#v", got.Endpoints)
	}
	if got.Endpoints[0].ID != "submit" || got.Endpoints[0].Title != "Submit Contact Form" {
		t.Fatalf("unexpected endpoint definition: %#v", got.Endpoints[0])
	}
}

func TestSubmitEndpointStoresEntryAndRedirects(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:         "Support Request",
		MailTo:       "support@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set("form_id", fmt.Sprint(form.ID))
	values.Set("name", "Jane")
	values.Set("email", "jane@example.com")
	values.Set("message", "Hello from the endpoint.")
	values.Set("return_url", "/contact")
	values.Set("_csrf", "submit-token")
	resp := postWire(t, server, "/endpoint/submit", extension.WireRequest{
		Method:       http.MethodPost,
		URL:          "/contact/submit",
		Body:         values.Encode(),
		SiteID:       "example",
		EndpointItem: "submit",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect status %d, got %d", http.StatusSeeOther, resp.StatusCode)
	}
	if loc := resp.Header["Location"]; len(loc) != 1 || loc[0] != "/contact?sent=1" {
		t.Fatalf("expected redirect to /contact?sent=1, got %#v", resp.Header)
	}

	var entry contactEntry
	if err := db.Table("contact_entries").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.FormID != form.ID {
		t.Fatalf("expected entry form id %d, got %d", form.ID, entry.FormID)
	}
	for _, want := range []string{`"name":"Jane"`, `"email":"jane@example.com"`, `"message":"Hello from the endpoint."`} {
		if !strings.Contains(entry.DataJSON, want) {
			t.Fatalf("expected entry data to contain %q, got %q", want, entry.DataJSON)
		}
	}
	if strings.Contains(entry.DataJSON, "form_id") {
		t.Fatalf("entry data should not duplicate form_id, got %q", entry.DataJSON)
	}
	if strings.Contains(entry.DataJSON, "return_url") {
		t.Fatalf("entry data should not store return_url, got %q", entry.DataJSON)
	}
	if strings.Contains(entry.DataJSON, "_csrf") {
		t.Fatalf("entry data should not store csrf token, got %q", entry.DataJSON)
	}
}

func TestSubmitDataRouteStoresEntryAndRedirects(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:         "Support Request",
		MailTo:       "support@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	actionURL := extension.PublicDataURL(extensionName, "example", "contact/submit")
	values := url.Values{}
	values.Set("form_id", fmt.Sprint(form.ID))
	values.Set("name", "Jane")
	values.Set("email", "jane@example.com")
	values.Set("message", "Hello from the data route.")
	values.Set("return_url", "/contact")
	resp := postWire(t, server, "/data/contact/submit", extension.WireRequest{
		Method:   http.MethodPost,
		URL:      actionURL,
		Body:     values.Encode(),
		SiteID:   "example",
		DataPath: "contact/submit",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected redirect status %d, got %d", http.StatusSeeOther, resp.StatusCode)
	}
	if loc := resp.Header["Location"]; len(loc) != 1 || loc[0] != "/contact?sent=1" {
		t.Fatalf("expected redirect to /contact?sent=1, got %#v", resp.Header)
	}

	var entry contactEntry
	if err := db.Table("contact_entries").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	if entry.FormID != form.ID {
		t.Fatalf("expected entry form id %d, got %d", form.ID, entry.FormID)
	}
	if !strings.Contains(entry.DataJSON, `"message":"Hello from the data route."`) {
		t.Fatalf("expected entry data from data route, got %q", entry.DataJSON)
	}
}

func TestSubmitValidatesSchemaFields(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:   "Validated",
		MailTo: "support@example.com",
		Status: "active",
		SchemaJSON: `{
		  "type": "object",
		  "properties": {
		    "email": {"type": "string", "format": "email"},
		    "section": {"type": "string", "enum": ["Section 1", "Section 2"]},
		    "count": {"type": "integer"},
		    "subscribe": {"type": "boolean"}
		  },
		  "required": ["email", "section", "subscribe"]
		}`,
		UISchemaJSON: `{
		  "type": "VerticalLayout",
		  "elements": [
		    {"type": "Control", "scope": "#/properties/email"},
		    {"type": "Control", "scope": "#/properties/section"},
		    {"type": "Control", "scope": "#/properties/count"},
		    {"type": "Control", "scope": "#/properties/subscribe"}
		  ]
		}`,
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	values := url.Values{}
	values.Set("form_id", fmt.Sprint(form.ID))
	values.Set("email", "not-an-email")
	values.Set("section", "Bogus")
	values.Set("count", "3.5")
	resp := postWire(t, server, "/endpoint/submit", extension.WireRequest{
		Method:       http.MethodPost,
		URL:          "/contact/submit",
		Body:         values.Encode(),
		SiteID:       "example",
		EndpointItem: "submit",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected validation status %d, got %+v", http.StatusBadRequest, resp)
	}

	values.Set("email", "jane@example.com")
	values.Set("section", "Section 2")
	values.Set("count", "3")
	values.Set("subscribe", "true")
	values.Set("unexpected", "ignore me")
	resp = postWire(t, server, "/endpoint/submit", extension.WireRequest{
		Method:       http.MethodPost,
		URL:          "/contact/submit",
		Body:         values.Encode(),
		SiteID:       "example",
		EndpointItem: "submit",
	})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected success redirect, got %+v", resp)
	}

	var entry contactEntry
	if err := db.Table("contact_entries").First(&entry).Error; err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"email":"jane@example.com"`, `"section":"Section 2"`, `"count":3`, `"subscribe":true`} {
		if !strings.Contains(entry.DataJSON, want) {
			t.Fatalf("expected entry data to contain %q, got %q", want, entry.DataJSON)
		}
	}
	if strings.Contains(entry.DataJSON, "unexpected") {
		t.Fatalf("entry data should not store unknown fields, got %q", entry.DataJSON)
	}
}

func TestBlockList(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/block", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	var got extension.BlockListResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode block list: %v", err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("expected one block definition, got %#v", got.Blocks)
	}
	if got.Blocks[0].ID != "contact-form" || got.Blocks[0].Title != "Contact Form" {
		t.Fatalf("unexpected block definition: %#v", got.Blocks[0])
	}
	if len(got.Blocks[0].Spaces) != 0 {
		t.Fatalf("contact form block should be available to any space, got %#v", got.Blocks[0].Spaces)
	}
}

func TestContactFormBlockRendersActiveForm(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	form := contactForm{
		Name:         "Support Request",
		ShowName:     true,
		Introduction: "Tell us what you need.",
		MailTo:       "support@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&form).Error; err != nil {
		t.Fatal(err)
	}

	resp := postWire(t, server, "/block/contact-form", extension.WireRequest{
		Method:     http.MethodGet,
		URL:        "/",
		CSRF:       "block-token",
		SiteID:     "example",
		BlockSpace: "footer",
		BlockItem:  "contact-form",
		BlockData: map[string]any{
			"form_id": float64(form.ID),
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected wire status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	for _, want := range []string{"Support Request", "Tell us what you need.", `name="email" type="email"`, `name="message"`, "<textarea", `name="_csrf" value="block-token"`, `<captcha context="form" provider="any"></captcha>`} {
		if !strings.Contains(resp.Body, want) {
			t.Fatalf("expected block body to contain %q, got %q", want, resp.Body)
		}
	}
}

func TestContactFormBlockWithoutFormIDIsEmpty(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	resp := postWire(t, server, "/block/contact-form", extension.WireRequest{
		Method:     http.MethodGet,
		URL:        "/",
		SiteID:     "example",
		BlockSpace: "footer",
		BlockItem:  "contact-form",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected wire status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if resp.Body != "" {
		t.Fatalf("expected empty body without form_id, got %q", resp.Body)
	}
}

func TestContactFormBlockWithoutFormIDRendersFirstActiveForm(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	inactive := contactForm{
		Name:         "Inactive Form",
		MailTo:       "old@example.com",
		Status:       "inactive",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&inactive).Error; err != nil {
		t.Fatal(err)
	}
	active := contactForm{
		Name:         "Primary Contact",
		ShowName:     true,
		MailTo:       "hello@example.com",
		Status:       "active",
		SchemaJSON:   defaultSchemaJSON(),
		UISchemaJSON: defaultUISchemaJSON(),
	}
	if err := db.Table("contact_forms").Create(&active).Error; err != nil {
		t.Fatal(err)
	}

	resp := postWire(t, server, "/block/contact-form", extension.WireRequest{
		Method:     http.MethodGet,
		URL:        "/",
		SiteID:     "example",
		BlockSpace: "footer",
		BlockItem:  "contact-form",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected wire status %d, got %d", http.StatusOK, resp.StatusCode)
	}
	if !strings.Contains(resp.Body, "Primary Contact") {
		t.Fatalf("expected first active form to render, got %q", resp.Body)
	}
	if strings.Contains(resp.Body, "Inactive Form") {
		t.Fatalf("expected inactive form to be skipped, got %q", resp.Body)
	}
}

func TestAdminListCreateAndDelete(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()

	install := postWire(t, server, "/install", extension.WireRequest{
		Method: http.MethodPost,
		URL:    "/install",
		SiteID: "example",
	})
	if install.StatusCode != http.StatusOK {
		t.Fatalf("install failed: %+v", install)
	}

	list := postWire(t, server, "/admin/", adminWire(http.MethodGet, adminURL("")))
	if !strings.Contains(list.Body, "No contact forms yet") {
		t.Fatalf("expected empty list, got %q", list.Body)
	}
	if !strings.Contains(list.Body, `/admin/configuration/extensions/`+extensionName) {
		t.Fatalf("expected settings link, got %q", list.Body)
	}

	form := url.Values{}
	form.Set("name", "General Contact")
	form.Set("mail_to", "hello@example.com")
	form.Set("introduction", "Send us a note.")
	form.Set("status", "active")
	form.Set("show_name", "1")
	form.Set("schema_json", defaultSchemaJSON())
	form.Set("ui_schema_json", defaultUISchemaJSON())
	created := postWire(t, server, "/admin/new", adminWireWithBody(http.MethodPost, adminURL("/new"), form.Encode()))
	if created.StatusCode != http.StatusOK {
		t.Fatalf("create failed: %+v", created)
	}
	if !strings.Contains(created.Body, "General Contact") {
		t.Fatalf("expected created form in list, got %q", created.Body)
	}
	if !strings.Contains(created.Body, `name="_csrf" value="admin-token"`) {
		t.Fatalf("expected admin list delete form to include csrf field, got %q", created.Body)
	}

	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	var rows []contactForm
	if err := db.Table("contact_forms").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 form row, got %d", len(rows))
	}
	if !rows[0].ShowName {
		t.Fatal("expected show_name to be saved as true")
	}

	deleted := postWire(t, server, fmt.Sprintf("/admin/%d/delete", rows[0].ID), adminWire(http.MethodPost, adminURL(fmt.Sprintf("/%d/delete", rows[0].ID))))
	if deleted.StatusCode != http.StatusOK {
		t.Fatalf("delete failed: %+v", deleted)
	}
	var count int64
	if err := db.Table("contact_forms").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected form to be deleted, count=%d", count)
	}
}

func TestAdminRejectsInvalidJSON(t *testing.T) {
	setupExtensionDB(t)
	server := newServer().Handler()
	postWire(t, server, "/install", extension.WireRequest{Method: http.MethodPost, URL: "/install", SiteID: "example"})

	form := url.Values{}
	form.Set("name", "Broken")
	form.Set("mail_to", "hello@example.com")
	form.Set("status", "active")
	form.Set("schema_json", "{")
	form.Set("ui_schema_json", defaultUISchemaJSON())
	resp := postWire(t, server, "/admin/new", adminWireWithBody(http.MethodPost, adminURL("/new"), form.Encode()))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected rendered validation response, got %+v", resp)
	}
	if !strings.Contains(resp.Body, "Schema JSON must be a valid JSON object.") {
		t.Fatalf("expected validation message, got %q", resp.Body)
	}
	if !strings.Contains(resp.Body, `name="_csrf" value="admin-token"`) {
		t.Fatalf("expected admin validation form to include csrf field, got %q", resp.Body)
	}
}

func TestContactFieldsUseUISchemaOrderAndStringEnums(t *testing.T) {
	form := contactForm{
		SchemaJSON: `{
		  "type": "object",
		  "properties": {
		    "name": {"type": "string"},
		    "email": {"type": "string", "format": "email"},
		    "city": {"type": "string"},
		    "section": {"type": "string", "enum": ["Section 1", "Section 2", "Section 3", "Section 4"]},
		    "message": {"type": "string", "description": "Tell us what you need."}
		  },
		  "required": ["name", "email", "city", "message"]
		}`,
		UISchemaJSON: `{
		  "type": "VerticalLayout",
		  "elements": [
		    {"type": "Control", "scope": "#/properties/name"},
		    {"type": "Control", "scope": "#/properties/email"},
		    {"type": "Control", "scope": "#/properties/city"},
		    {"type": "Control", "scope": "#/properties/section"},
		    {"type": "Control", "scope": "#/properties/message", "options": {"multi": true}}
		  ]
		}`,
	}

	fields := contactFields(form)
	if len(fields) != 5 {
		t.Fatalf("fields len = %d, want 5: %#v", len(fields), fields)
	}
	wantOrder := []string{"name", "email", "city", "section", "message"}
	for i, want := range wantOrder {
		if fields[i].Name != want {
			t.Fatalf("field %d = %q, want %q; fields=%#v", i, fields[i].Name, want, fields)
		}
	}
	if !fields[3].Select || len(fields[3].Options) != 4 || fields[3].Options[0].Value != "Section 1" {
		t.Fatalf("section field not rendered as enum select: %#v", fields[3])
	}
	if !fields[4].Textarea {
		t.Fatalf("message field should be textarea: %#v", fields[4])
	}
	if fields[4].Help != "Tell us what you need." {
		t.Fatalf("message help = %q", fields[4].Help)
	}
}

func TestContactFieldsSupportBasicSchemaTypesAndOptions(t *testing.T) {
	form := contactForm{
		SchemaJSON: `{
		  "type": "object",
		  "properties": {
		    "subscribe": {"type": "boolean", "title": "Subscribe"},
		    "attendees": {"type": "integer"},
		    "rating": {"type": "number"},
		    "event_date": {"type": "string", "format": "date"},
		    "choice": {"type": "string", "oneOf": [
		      {"const": "a", "title": "Option A"},
		      {"const": "b", "title": "Option B"}
		    ]}
		  },
		  "required": ["subscribe"]
		}`,
		UISchemaJSON: `{
		  "type": "VerticalLayout",
		  "elements": [
		    {"type": "Control", "scope": "#/properties/subscribe"},
		    {"type": "Control", "scope": "#/properties/attendees"},
		    {"type": "Control", "scope": "#/properties/rating"},
		    {"type": "Control", "scope": "#/properties/event_date", "options": {"placeholder": "YYYY-MM-DD"}},
		    {"type": "Control", "scope": "#/properties/choice", "label": "Choice Label"}
		  ]
		}`,
	}

	fields := contactFields(form)
	if len(fields) != 5 {
		t.Fatalf("fields len = %d, want 5: %#v", len(fields), fields)
	}
	if !fields[0].Boolean || fields[0].Type != "checkbox" || !fields[0].Required {
		t.Fatalf("unexpected boolean field: %#v", fields[0])
	}
	if fields[1].Type != "number" || fields[1].Step != "1" {
		t.Fatalf("unexpected integer field: %#v", fields[1])
	}
	if fields[2].Type != "number" || fields[2].Step != "any" {
		t.Fatalf("unexpected number field: %#v", fields[2])
	}
	if fields[3].Type != "date" || fields[3].Placeholder != "YYYY-MM-DD" {
		t.Fatalf("unexpected date field: %#v", fields[3])
	}
	if !fields[4].Select || fields[4].Label != "Choice Label" || fields[4].Options[0].Label != "Option A" {
		t.Fatalf("unexpected oneOf select field: %#v", fields[4])
	}
}

func TestContactFieldsUseSchemaDefaults(t *testing.T) {
	form := contactForm{
		SchemaJSON: `{
		  "type": "object",
		  "properties": {
		    "name": {"type": "string", "default": "Ada"},
		    "section": {"type": "string", "default": "Section 1", "enum": ["I don't know", "Section 1", "Section 2"]},
		    "subscribe": {"type": "boolean", "default": true},
		    "attendees": {"type": "integer", "default": 3}
		  }
		}`,
		UISchemaJSON: `{
		  "type": "VerticalLayout",
		  "elements": [
		    {"type": "Control", "scope": "#/properties/name"},
		    {"type": "Control", "scope": "#/properties/section"},
		    {"type": "Control", "scope": "#/properties/subscribe"},
		    {"type": "Control", "scope": "#/properties/attendees"}
		  ]
		}`,
	}

	fields := contactFields(form)
	if len(fields) != 4 {
		t.Fatalf("fields len = %d, want 4: %#v", len(fields), fields)
	}
	if fields[0].Default != "Ada" {
		t.Fatalf("name default = %q", fields[0].Default)
	}
	if !fields[1].Select || len(fields[1].Options) != 3 || !fields[1].Options[1].Selected {
		t.Fatalf("section default option not selected: %#v", fields[1])
	}
	if !fields[2].Checked {
		t.Fatalf("boolean default should be checked: %#v", fields[2])
	}
	if fields[3].Default != "3" {
		t.Fatalf("integer default = %q", fields[3].Default)
	}
}

func TestHelp(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/help", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var got struct {
		Help []string `json:"help"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := []string{"/help/overview", "/help/contact"}
	if len(got.Help) != len(want) {
		t.Fatalf("expected help entries %#v, got %#v", want, got.Help)
	}
	for i := range want {
		if got.Help[i] != want[i] {
			t.Fatalf("expected help entries %#v, got %#v", want, got.Help)
		}
	}
}

func TestHelpArticle(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/help/overview", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"# Contact Extension", "Shoutrrr notification URL", "endpoint submission handling"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected overview markdown to contain %q, got %q", want, body)
		}
	}
}

func TestMissingHelpArticle(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/help/missing", nil)
	rec := httptest.NewRecorder()

	newServer().Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rec.Code)
	}
}

func TestMigrateTablesAddsShowNameOnExistingSQLiteTable(t *testing.T) {
	setupExtensionDB(t)
	db, _, err := extension.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	app := &contactApp{info: extension.Info{Name: extensionName}, settingsTable: "contact_settings"}
	if err := db.Exec(`CREATE TABLE contact_forms (
		form_id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		introduction TEXT,
		mail_to TEXT NOT NULL,
		schema_json TEXT NOT NULL,
		ui_schema_json TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active',
		created_at DATETIME,
		updated_at DATETIME
	)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO contact_forms (name, mail_to, schema_json, ui_schema_json, status)
		VALUES ('Legacy', 'a@example.com', '{}', '{}', 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := app.migrateTables(db); err != nil {
		t.Fatalf("migrateTables: %v", err)
	}
	if !sqliteColumnExists(db, "contact_forms", "show_name") {
		t.Fatal("expected show_name column after migrate")
	}
	var showName bool
	if err := db.Raw(`SELECT show_name FROM contact_forms WHERE name = ?`, "Legacy").Scan(&showName).Error; err != nil {
		t.Fatal(err)
	}
	if !showName {
		t.Fatal("expected existing rows to default show_name = true")
	}
}

func setupExtensionDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sites.json")
	dbPath := filepath.Join(dir, "example.sqlite")
	raw := fmt.Sprintf(`{
  "install_enabled": false,
  "data_root": %q,
  "sites": [{
    "id": "example",
    "name": "Example",
    "host": "http://localhost:8001",
    "database": {"type": "sqlite", "dsn": %q},
    "template_dir": %q,
    "assets_dir": %q,
    "tmp_dir": %q,
    "language_dir": %q
  }]
}`, dir, dbPath, filepath.Join(dir, "templates"), filepath.Join(dir, "assets"), filepath.Join(dir, "tmp"), filepath.Join(dir, "language"))
	if err := os.WriteFile(cfgPath, []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CANNON_CONFIG", cfgPath)
	oldArgs := os.Args
	os.Args = []string{"contact-extension", "--site=example", "--socket=/tmp/contact.sock"}
	t.Cleanup(func() { os.Args = oldArgs })
}

func postWire(t *testing.T, handler http.Handler, path string, wire extension.WireRequest) extension.WireResponse {
	t.Helper()
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP status %d, got %d: %s", http.StatusOK, rec.Code, rec.Body.String())
	}
	var resp extension.WireResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode wire response: %v", err)
	}
	return resp
}

func adminWire(method, rawURL string) extension.WireRequest {
	return adminWireWithBody(method, rawURL, "")
}

func adminWireWithBody(method, rawURL, body string) extension.WireRequest {
	return extension.WireRequest{
		Method: method,
		URL:    rawURL,
		Body:   body,
		CSRF:   "admin-token",
		SiteID: "example",
		User: map[string]any{
			"authenticated": true,
			"given_name":    "Jane",
			"family_name":   "Doe",
			"email":         "jane@example.com",
		},
	}
}

func adminURL(suffix string) string {
	return "/admin/extension-apps/" + extensionName + suffix
}
