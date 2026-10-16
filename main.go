package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/containrrr/shoutrrr"
	"github.com/rob121/cannon/extension"
	"gorm.io/gorm"
)

//go:embed help/*.md
var helpFiles embed.FS

//go:embed all:templates
var templateFiles embed.FS

const (
	extensionName        = "cannon-ext-contact"
	extensionTitle       = "Contact Forms"
	extensionDescription = "Adds configurable contact forms with JSON Forms schemas, page and block rendering, saved submissions, and optional Shoutrrr notifications."
	extensionVersion     = "0.2.0"
	updateURLBase        = "https://github.com/rob121/cannon-ext-contact/releases/download"
	defaultAdminMenu     = "Contact Forms"
	pageSize             = 10
)

var contactConfigSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "shoutrrr_url": {
      "type": "string",
      "title": "Shoutrrr URL",
      "format": "uri",
      "description": "A Shoutrrr notification URL used to send contact form submissions."
    }
  }
}`)

var contactConfigUI = json.RawMessage(`{
  "type": "VerticalLayout",
  "elements": [
    {
      "type": "Control",
      "scope": "#/properties/shoutrrr_url"
    }
  ]
}`)

type contactApp struct {
	info          extension.Info
	templates     *extension.Templates
	settingsTable string
}

type contactForm struct {
	ID           uint      `gorm:"primaryKey;column:form_id"`
	Name         string    `gorm:"column:name;size:160;not null"`
	ShowName     bool      `gorm:"column:show_name;not null"`
	Introduction string    `gorm:"column:introduction;type:text"`
	MailTo       string    `gorm:"column:mail_to;size:320;not null"`
	SchemaJSON   string    `gorm:"column:schema_json;type:text;not null"`
	UISchemaJSON string    `gorm:"column:ui_schema_json;type:text;not null"`
	Status       string    `gorm:"column:status;size:16;not null;default:active"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type contactEntry struct {
	ID        uint      `gorm:"primaryKey;column:entry_id"`
	FormID    uint      `gorm:"column:form_id;index;not null"`
	DataJSON  string    `gorm:"column:data_json;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type contactSettings struct {
	ShoutrrrURL string `json:"shoutrrr_url"`
}

type contactField struct {
	Name        string
	Label       string
	Type        string
	SchemaType  string
	Default     string
	Checked     bool
	Textarea    bool
	Select      bool
	Boolean     bool
	Options     []contactFieldOption
	Required    bool
	Readonly    bool
	Placeholder string
	Help        string
	Step        string
}

type contactFieldOption struct {
	Value    string
	Label    string
	Selected bool
}

type schemaDocument struct {
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type schemaProperty struct {
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Format      string         `json:"format"`
	Default     any            `json:"default"`
	Enum        []any          `json:"enum"`
	OneOf       []schemaChoice `json:"oneOf"`
	AnyOf       []schemaChoice `json:"anyOf"`
	Const       any            `json:"const"`
}

type schemaChoice struct {
	Const any    `json:"const"`
	Title string `json:"title"`
}

type uiElement struct {
	Type     string         `json:"type"`
	Scope    string         `json:"scope"`
	Label    any            `json:"label"`
	Options  map[string]any `json:"options"`
	Elements []uiElement    `json:"elements"`
}

func main() {
	if err := newServer().Run(); err != nil {
		log.Fatal(err)
	}
}

func newServer() *extension.Server {
	info := extension.Info{
		Name:          extensionName,
		Title:         extensionTitle,
		Description:   extensionDescription,
		Version:       extensionVersion,
		UpdateURLBase: updateURLBase,
		AdminMenuName: defaultAdminMenu,
	}
	app := &contactApp{
		info:          info,
		templates:     extension.NewTemplates(templateFiles, "templates"),
		settingsTable: extension.TableName(info.TablePrefix(), "settings"),
	}

	s := extension.New(info)
	s.EmbedHelp(helpFiles, "help")
	s.EmbedTemplates(templateFiles, "templates")
	s.OnInstall(app.install)
	s.OnConfiguration(extension.MapConfiguration([]extension.ConfigurationDefinition{
		{
			ID:       "general",
			Title:    "General",
			Schema:   contactConfigSchema,
			UISchema: contactConfigUI,
		},
	}, configStore{table: app.settingsTable}))
	s.RegisterPage(extension.PageDefinition{
		ID:    "contact-form",
		Title: "Contact Form",
	}, app.page)
	s.OnPageList(app.pageDefinitions)
	s.RegisterEndpoint(extension.EndpointDefinition{
		ID:    "submit",
		Title: "Submit Contact Form",
	}, app.submit)
	s.HandleData("contact/submit", func(req extension.WireRequest) extension.WireResponse {
		return app.submit("contact/submit", req)
	})
	s.HandleAdmin("/admin/", app.admin)
	s.RegisterBlock(extension.BlockDefinition{
		ID:    "contact-form",
		Title: "Contact Form",
	}, app.block)
	s.OnBlockList(app.blockDefinitions)

	return s
}

func (app *contactApp) install(req extension.WireRequest) error {
	db, _, err := extension.OpenDB()
	if err != nil {
		return err
	}
	if err := extension.MigrateConfigurationStore(db, app.settingsTable); err != nil {
		return err
	}
	if err := app.migrateTables(db); err != nil {
		return err
	}
	return nil
}

func (app *contactApp) migrateTables(db *gorm.DB) error {
	table := app.formsTable()
	if db.Migrator().HasTable(table) && !sqliteColumnExists(db, table, "show_name") {
		// SQLite rejects ADD COLUMN … NOT NULL without DEFAULT when rows already exist.
		quoted := `"` + strings.ReplaceAll(table, `"`, `""`) + `"`
		if err := db.Exec(`ALTER TABLE ` + quoted + ` ADD COLUMN show_name INTEGER NOT NULL DEFAULT 1`).Error; err != nil {
			return err
		}
	}
	if err := db.Table(table).AutoMigrate(&contactForm{}); err != nil {
		return err
	}
	return db.Table(app.entriesTable()).AutoMigrate(&contactEntry{})
}

func sqliteColumnExists(db *gorm.DB, table, column string) bool {
	var count int64
	// Works on SQLite; for other drivers fall back to Migrator after AutoMigrate would have run.
	err := db.Raw(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count).Error
	if err == nil {
		return count > 0
	}
	type colModel struct{}
	return db.Table(table).Migrator().HasColumn(&contactForm{}, "ShowName")
}

func (app *contactApp) pageDefinitions() ([]extension.PageDefinition, error) {
	return []extension.PageDefinition{
		{
			ID:    "contact-form",
			Title: "Contact Form",
			Fields: []extension.PageField{
				{
					Name:     "form_id",
					Label:    "Contact Form",
					Type:     "select",
					Help:     "Active contact form rendered by this route.",
					Required: true,
					Options:  app.activeFormOptions(),
				},
			},
		},
	}, nil
}

func (app *contactApp) page(item string, req extension.WireRequest) extension.WireResponse {
	formID, ok := extension.PageDataInt(req, "form_id")
	form, found, err := app.activeForm(formID, ok && formID > 0)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return extension.HTML(http.StatusOK, "")
	}

	return app.render("contact/form.html", map[string]any{
		"Form":      form,
		"Fields":    contactFields(form),
		"ActionURL": extension.PublicDataURL(extensionName, req.SiteID, "contact/submit"),
		"ReturnURL": req.URL,
		"CSRFField": csrfField(req),
		"PageItem":  item,
		"BlockItem": "",
	})
}

func (app *contactApp) blockDefinitions() ([]extension.BlockDefinition, error) {
	fields := []extension.BlockField{
		{
			Name:     "form_id",
			Label:    "Contact Form",
			Type:     "select",
			Help:     "Active contact form rendered by this block.",
			Required: false,
			Options:  app.activeFormOptions(),
		},
	}
	return []extension.BlockDefinition{
		{
			ID:     "contact-form",
			Title:  "Contact Form",
			Fields: fields,
		},
	}, nil
}

func (app *contactApp) activeFormOptions() []extension.BlockFieldOption {
	db, _, err := extension.OpenDB()
	if err != nil {
		return nil
	}
	var forms []contactForm
	err = db.Table(app.formsTable()).
		Where("status = ?", "active").
		Order("name asc, form_id asc").
		Find(&forms).Error
	if err != nil {
		return nil
	}
	options := make([]extension.BlockFieldOption, 0, len(forms))
	for _, form := range forms {
		options = append(options, extension.BlockFieldOption{
			Value: strconv.FormatUint(uint64(form.ID), 10),
			Label: form.Name,
		})
	}
	return options
}

func (app *contactApp) block(item string, req extension.WireRequest) extension.WireResponse {
	formID, ok := extension.BlockDataInt(req, "form_id")
	form, found, err := app.activeForm(formID, ok && formID > 0)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return extension.HTML(http.StatusOK, "")
	}

	return app.render("contact/block.html", map[string]any{
		"Form":      form,
		"Fields":    contactFields(form),
		"ActionURL": extension.PublicDataURL(extensionName, req.SiteID, "contact/submit"),
		"ReturnURL": req.URL,
		"CSRFField": csrfField(req),
		"Space":     extension.BlockSpace(req),
		"BlockItem": item,
	})
}

func (app *contactApp) submit(item string, req extension.WireRequest) extension.WireResponse {
	if !strings.EqualFold(req.Method, http.MethodPost) {
		return extension.Error(http.StatusMethodNotAllowed, "POST required")
	}

	values, err := url.ParseQuery(req.Body)
	if err != nil {
		return extension.Error(http.StatusBadRequest, "invalid form submission")
	}
	formID, err := strconv.Atoi(strings.TrimSpace(values.Get("form_id")))
	if err != nil || formID <= 0 {
		return extension.Error(http.StatusBadRequest, "form_id is required")
	}

	form, found, err := app.activeForm(formID, true)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if !found {
		return extension.Error(http.StatusNotFound, "contact form not found")
	}

	data, err := validatedContactData(form, values)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	db, _, err := extension.OpenDB()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	entry := contactEntry{
		FormID:   form.ID,
		DataJSON: string(raw),
	}
	if err := db.Table(app.entriesTable()).Create(&entry).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	settings, err := loadContactSettings()
	if err == nil && settings.ShoutrrrURL != "" {
		if err := sendContactNotification(settings, form, entry); err != nil {
			return extension.Error(http.StatusInternalServerError, err.Error())
		}
	}

	redirectURL := strings.TrimSpace(values.Get("return_url"))
	if redirectURL == "" {
		redirectURL = req.URL
	}
	return extension.Redirect(http.StatusSeeOther, contactSubmitRedirect(redirectURL))
}

func (app *contactApp) activeForm(formID int, requireID bool) (contactForm, bool, error) {
	db, _, err := extension.OpenDB()
	if err != nil {
		return contactForm{}, false, err
	}
	var form contactForm
	query := db.Table(app.formsTable()).Where("status = ?", "active")
	if requireID {
		query = query.Where("form_id = ?", formID)
	} else {
		query = query.Order("form_id asc")
	}
	err = query.First(&form).Error
	if err == gorm.ErrRecordNotFound {
		return contactForm{}, false, nil
	}
	if err != nil {
		return contactForm{}, false, err
	}
	return form, true, nil
}

func (app *contactApp) admin(req extension.WireRequest) extension.WireResponse {
	if !extension.UserAuthenticated(req) {
		return extension.Error(http.StatusForbidden, "sign in required")
	}

	db, _, err := extension.OpenDB()
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	if err := app.migrateTables(db); err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	adminURL, err := parseWireURL(req.URL)
	if err != nil {
		return extension.Error(http.StatusBadRequest, err.Error())
	}
	basePath, parts := adminRoute(adminURL.Path)
	if basePath == "" {
		basePath = "/admin/extension-apps/" + url.PathEscape(extensionName)
	}

	switch {
	case len(parts) == 0:
		return app.listForms(db, adminURL, basePath, "", csrfField(req))
	case len(parts) == 1 && parts[0] == "new":
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveForm(db, req.Body, adminURL, basePath, 0, csrfField(req))
		}
		return app.renderForm(basePath, contactForm{
			Status:       "active",
			ShowName:     true,
			SchemaJSON:   defaultSchemaJSON(),
			UISchemaJSON: defaultUISchemaJSON(),
		}, true, "", csrfField(req))
	case len(parts) == 1:
		id, ok := parseID(parts[0])
		if !ok {
			return extension.Error(http.StatusNotFound, "form not found")
		}
		if strings.EqualFold(req.Method, http.MethodPost) {
			return app.saveForm(db, req.Body, adminURL, basePath, id, csrfField(req))
		}
		return app.editForm(db, basePath, id, "", csrfField(req))
	case len(parts) == 2 && parts[1] == "delete":
		id, ok := parseID(parts[0])
		if !ok {
			return extension.Error(http.StatusNotFound, "form not found")
		}
		if !strings.EqualFold(req.Method, http.MethodPost) {
			return extension.Error(http.StatusMethodNotAllowed, "method not allowed")
		}
		return app.deleteForm(db, adminURL, basePath, id, csrfField(req))
	default:
		return extension.Error(http.StatusNotFound, "admin route not found")
	}
}

func (app *contactApp) listForms(db *gorm.DB, adminURL *url.URL, basePath, notice string, csrf template.HTML) extension.WireResponse {
	page := parsePage(adminURL)
	var total int64
	if err := db.Table(app.formsTable()).Count(&total).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	var activeTotal int64
	if err := db.Table(app.formsTable()).Where("status = ?", "active").Count(&activeTotal).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	var entryTotal int64
	if err := db.Table(app.entriesTable()).Count(&entryTotal).Error; err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	var rows []contactForm
	err := db.Table(app.formsTable()).
		Order("created_at desc, form_id desc").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}

	return app.render("contact/admin_list.html", map[string]any{
		"Title":           "Contact Forms",
		"Subtitle":        "Create and manage contact form definitions.",
		"PageActionURL":   basePath + "/new",
		"PageActionLabel": "Add Form",
		"SettingsURL":     "/admin/configuration/extensions/" + url.PathEscape(extensionName),
		"BasePath":        basePath,
		"Rows":            rows,
		"Page":            page,
		"PageSize":        pageSize,
		"Total":           total,
		"ActiveTotal":     activeTotal,
		"EntryTotal":      entryTotal,
		"Notice":          notice,
		"CSRFField":       csrf,
	})
}

func (app *contactApp) editForm(db *gorm.DB, basePath string, id uint, errMsg string, csrf template.HTML) extension.WireResponse {
	var row contactForm
	err := db.Table(app.formsTable()).Where("form_id = ?", id).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return extension.Error(http.StatusNotFound, "form not found")
	}
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.renderForm(basePath, row, false, errMsg, csrf)
}

func (app *contactApp) saveForm(db *gorm.DB, body string, adminURL *url.URL, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	row, errMsg := formFromBody(body, id)
	if errMsg != "" {
		return app.renderForm(basePath, row, id == 0, errMsg, csrf)
	}

	if id != 0 {
		var existing contactForm
		err := db.Table(app.formsTable()).Where("form_id = ?", id).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			return extension.Error(http.StatusNotFound, "form not found")
		}
		if err != nil {
			return extension.Error(http.StatusInternalServerError, err.Error())
		}
		row.ID = id
		row.CreatedAt = existing.CreatedAt
	}

	if err := db.Table(app.formsTable()).Save(&row).Error; err != nil {
		return app.renderForm(basePath, row, id == 0, err.Error(), csrf)
	}
	return app.listForms(db, adminURL, basePath, "Form saved.", csrf)
}

func (app *contactApp) deleteForm(db *gorm.DB, adminURL *url.URL, basePath string, id uint, csrf template.HTML) extension.WireResponse {
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Table(app.entriesTable()).Where("form_id = ?", id).Delete(&contactEntry{}).Error; err != nil {
			return err
		}
		return tx.Table(app.formsTable()).Where("form_id = ?", id).Delete(&contactForm{}).Error
	})
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return app.listForms(db, adminURL, basePath, "Form deleted.", csrf)
}

func (app *contactApp) renderForm(basePath string, row contactForm, isNew bool, errMsg string, csrf template.HTML) extension.WireResponse {
	title := "Edit Contact Form"
	action := fmt.Sprintf("%s/%d", basePath, row.ID)
	if isNew {
		title = "Add Contact Form"
		action = basePath + "/new"
	}
	return app.render("contact/admin_form.html", map[string]any{
		"Title":     title,
		"Subtitle":  "Configure delivery settings and JSON Forms schemas.",
		"BasePath":  basePath,
		"ActionURL": action,
		"CancelURL": basePath,
		"Row":       row,
		"IsNew":     isNew,
		"Error":     errMsg,
		"CSRFField": csrf,
	})
}

func csrfField(req extension.WireRequest) template.HTML {
	return template.HTML(extension.CSRFHiddenField(req))
}

func (app *contactApp) render(name string, data map[string]any) extension.WireResponse {
	body, err := app.templates.WithFuncs(template.FuncMap{
		"add":       func(a, b int) int { return a + b },
		"sub":       func(a, b int) int { return a - b },
		"mul":       func(a, b int) int { return a * b },
		"pageCount": pageCount,
	}).Execute(name, data)
	if err != nil {
		return extension.Error(http.StatusInternalServerError, err.Error())
	}
	return extension.HTML(http.StatusOK, body)
}

func (app *contactApp) formsTable() string {
	return extension.TableName(app.info.TablePrefix(), "forms")
}

func (app *contactApp) entriesTable() string {
	return extension.TableName(app.info.TablePrefix(), "entries")
}

type configStore struct {
	table string
}

func (s configStore) Load(section string) (map[string]any, error) {
	db, _, err := extension.OpenDB()
	if err != nil {
		return nil, err
	}
	if err := extension.MigrateConfigurationStore(db, s.table); err != nil {
		return nil, err
	}
	return extension.DBConfigurationStore(db, s.table).Load(section)
}

func (s configStore) Save(section string, data map[string]any) error {
	db, _, err := extension.OpenDB()
	if err != nil {
		return err
	}
	if err := extension.MigrateConfigurationStore(db, s.table); err != nil {
		return err
	}
	return extension.DBConfigurationStore(db, s.table).Save(section, data)
}

func loadContactSettings() (contactSettings, error) {
	store := configStore{table: extension.TableName(extension.Info{Name: extensionName}.TablePrefix(), "settings")}
	data, err := store.Load("general")
	if err != nil {
		return contactSettings{}, err
	}
	return contactSettings{
		ShoutrrrURL: strings.TrimSpace(stringValue(data, "shoutrrr_url")),
	}, nil
}

func sendContactNotification(settings contactSettings, form contactForm, entry contactEntry) error {
	if settings.ShoutrrrURL == "" {
		return fmt.Errorf("shoutrrr_url is not configured")
	}
	message := fmt.Sprintf("New contact form submission for %s\n\n%s", form.Name, entry.DataJSON)
	return shoutrrr.Send(settings.ShoutrrrURL, message)
}

func contactSubmitRedirect(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || strings.TrimSpace(u.Path) == "" {
		return "/?sent=1"
	}
	path := strings.TrimSuffix(u.Path, "/")
	if strings.HasSuffix(path, "/submit") {
		path = strings.TrimSuffix(path, "/submit")
	}
	if path == "" {
		path = "/"
	}
	q := u.Query()
	q.Set("sent", "1")
	u.RawQuery = q.Encode()
	u.Path = path
	return u.String()
}

func formFromBody(body string, id uint) (contactForm, string) {
	values, err := url.ParseQuery(body)
	if err != nil {
		return contactForm{}, err.Error()
	}
	showName := values.Get("show_name")
	row := contactForm{
		ID:           id,
		Name:         strings.TrimSpace(values.Get("name")),
		ShowName:     showName == "1" || strings.EqualFold(showName, "on") || strings.EqualFold(showName, "true") || strings.EqualFold(showName, "yes"),
		Introduction: strings.TrimSpace(values.Get("introduction")),
		MailTo:       strings.TrimSpace(values.Get("mail_to")),
		SchemaJSON:   strings.TrimSpace(values.Get("schema_json")),
		UISchemaJSON: strings.TrimSpace(values.Get("ui_schema_json")),
		Status:       strings.TrimSpace(values.Get("status")),
	}
	if row.Status != "inactive" {
		row.Status = "active"
	}
	if row.Name == "" {
		return row, "Form name is required."
	}
	if row.MailTo == "" {
		return row, "Mail To is required."
	}
	if row.SchemaJSON == "" {
		row.SchemaJSON = defaultSchemaJSON()
	}
	if row.UISchemaJSON == "" {
		row.UISchemaJSON = defaultUISchemaJSON()
	}
	if !validJSONObject(row.SchemaJSON) {
		return row, "Schema JSON must be a valid JSON object."
	}
	if !validJSONObject(row.UISchemaJSON) {
		return row, "UI schema JSON must be a valid JSON object."
	}
	return row, ""
}

func parseWireURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return url.Parse("/admin/extension-apps/" + url.PathEscape(extensionName))
	}
	return url.Parse(raw)
}

func adminRoute(path string) (string, []string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i+2 < len(parts); i++ {
		if parts[i] == "admin" && parts[i+1] == "extension-apps" {
			base := "/" + strings.Join(parts[:i+3], "/")
			return base, parts[i+3:]
		}
	}
	return "", nil
}

func parseID(value string) (uint, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

func parsePage(u *url.URL) int {
	page, err := strconv.Atoi(u.Query().Get("page"))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func pageCount(total int64, size int) int {
	if total <= 0 || size <= 0 {
		return 1
	}
	return int((total + int64(size) - 1) / int64(size))
}

func validJSONObject(raw string) bool {
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

func stringValue(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, _ := data[key].(string)
	return value
}

func contactFields(form contactForm) []contactField {
	var schema schemaDocument
	if err := json.Unmarshal([]byte(form.SchemaJSON), &schema); err != nil {
		return nil
	}
	required := map[string]bool{}
	for _, name := range schema.Required {
		required[name] = true
	}
	controls := uiControls(form.UISchemaJSON)
	fields := make([]contactField, 0, len(schema.Properties))
	seen := map[string]bool{}
	for _, control := range controls {
		name := propertyName(control.Scope)
		if name == "" || seen[name] {
			continue
		}
		prop, ok := schema.Properties[name]
		if !ok {
			continue
		}
		options := control.Options
		if _, ok := options["label"]; !ok && control.Label != nil {
			options = copyOptions(options)
			options["label"] = control.Label
		}
		if field, ok := buildContactField(name, prop, options, required[name]); ok {
			fields = append(fields, field)
			seen[name] = true
		}
	}
	if len(controls) == 0 {
		for name, prop := range schema.Properties {
			if field, ok := buildContactField(name, prop, nil, required[name]); ok {
				fields = append(fields, field)
			}
		}
	}
	return fields
}

func validatedContactData(form contactForm, values url.Values) (map[string]any, error) {
	fields := contactFields(form)
	data := make(map[string]any, len(fields))
	for _, field := range fields {
		value, err := contactFieldValue(field, values)
		if err != nil {
			return nil, err
		}
		if value != nil {
			data[field.Name] = value
		}
	}
	return data, nil
}

func contactFieldValue(field contactField, values url.Values) (any, error) {
	raw := strings.TrimSpace(values.Get(field.Name))
	if field.Boolean {
		checked := raw == "1" || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "on") || strings.EqualFold(raw, "yes")
		if field.Required && !checked {
			return nil, fmt.Errorf("%s is required", field.Label)
		}
		return checked, nil
	}
	if field.Required && raw == "" {
		return nil, fmt.Errorf("%s is required", field.Label)
	}
	if raw == "" {
		return nil, nil
	}
	if len(field.Options) > 0 && !fieldAllowsOption(field, raw) {
		return nil, fmt.Errorf("%s has an invalid value", field.Label)
	}
	switch field.SchemaType {
	case "integer":
		value, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be a whole number", field.Label)
		}
		return value, nil
	case "number":
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%s must be a number", field.Label)
		}
		return value, nil
	default:
		if err := validateStringFormat(field, raw); err != nil {
			return nil, err
		}
		return raw, nil
	}
}

func fieldAllowsOption(field contactField, value string) bool {
	for _, option := range field.Options {
		if option.Value == value {
			return true
		}
	}
	return false
}

func validateStringFormat(field contactField, value string) error {
	switch field.Type {
	case "email":
		if _, err := mail.ParseAddress(value); err != nil {
			return fmt.Errorf("%s must be a valid email address", field.Label)
		}
	case "url":
		u, err := url.Parse(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("%s must be a valid URL", field.Label)
		}
	case "date":
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return fmt.Errorf("%s must be a valid date", field.Label)
		}
	case "datetime-local":
		if _, err := time.Parse("2006-01-02T15:04", value); err != nil {
			return fmt.Errorf("%s must be a valid date and time", field.Label)
		}
	}
	return nil
}

func buildContactField(name string, prop schemaProperty, options map[string]any, required bool) (contactField, bool) {
	schemaType := strings.TrimSpace(prop.Type)
	if schemaType == "" {
		schemaType = "string"
	}
	if schemaType != "string" && schemaType != "boolean" && schemaType != "integer" && schemaType != "number" {
		return contactField{}, false
	}
	inputType := "text"
	step := ""
	switch schemaType {
	case "boolean":
		inputType = "checkbox"
	case "integer":
		inputType = "number"
		step = "1"
	case "number":
		inputType = "number"
		step = "any"
	}
	if prop.Format == "email" {
		inputType = "email"
	}
	if prop.Format == "uri" {
		inputType = "url"
	}
	if prop.Format == "date" {
		inputType = "date"
	}
	if prop.Format == "date-time" {
		inputType = "datetime-local"
	}
	if prop.Format == "tel" || prop.Format == "phone" {
		inputType = "tel"
	}
	if optionFormat := stringOption(options, "format"); optionFormat == "password" {
		inputType = "password"
	}
	defaultValue := defaultFieldValue(prop.Default)
	field := contactField{
		Name:        name,
		Label:       fieldLabel(name, prop.Title),
		Type:        inputType,
		SchemaType:  schemaType,
		Default:     defaultValue,
		Checked:     defaultBoolValue(prop.Default),
		Boolean:     schemaType == "boolean",
		Textarea:    boolOption(options, "multi"),
		Required:    required,
		Readonly:    boolOption(options, "readonly") || boolOption(options, "readOnly"),
		Placeholder: stringOption(options, "placeholder"),
		Help:        prop.Description,
		Step:        step,
	}
	if label, ok := stringLabel(options, "label"); ok {
		field.Label = label
	}
	for _, value := range prop.Enum {
		label := fmt.Sprint(value)
		field.Options = append(field.Options, contactFieldOption{Value: label, Label: label, Selected: label == defaultValue})
	}
	for _, choice := range append(prop.OneOf, prop.AnyOf...) {
		if choice.Const == nil {
			continue
		}
		value := fmt.Sprint(choice.Const)
		label := choice.Title
		if strings.TrimSpace(label) == "" {
			label = value
		}
		field.Options = append(field.Options, contactFieldOption{Value: value, Label: label, Selected: value == defaultValue})
	}
	if len(field.Options) > 0 {
		field.Select = true
		field.Textarea = false
		field.Boolean = false
		field.Type = "select"
	}
	return field, true
}

func defaultFieldValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func defaultBoolValue(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true") || strings.EqualFold(v, "on") || strings.EqualFold(v, "yes") || v == "1"
	case float64:
		return v != 0
	default:
		return false
	}
}

func copyOptions(options map[string]any) map[string]any {
	out := make(map[string]any, len(options)+1)
	for key, value := range options {
		out[key] = value
	}
	return out
}

func uiControls(raw string) []uiElement {
	var root uiElement
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	var out []uiElement
	walkUI(root, &out)
	return out
}

func walkUI(el uiElement, out *[]uiElement) {
	if propertyName(el.Scope) != "" {
		*out = append(*out, el)
	}
	for _, child := range el.Elements {
		walkUI(child, out)
	}
}

func propertyName(scope string) string {
	name := strings.TrimPrefix(scope, "#/properties/")
	if name == scope || name == "" {
		return ""
	}
	return name
}

func boolOption(options map[string]any, key string) bool {
	v, _ := options[key].(bool)
	return v
}

func stringOption(options map[string]any, key string) string {
	v, _ := options[key].(string)
	return strings.TrimSpace(v)
}

func stringLabel(options map[string]any, key string) (string, bool) {
	raw, ok := options[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), strings.TrimSpace(v) != ""
	case bool:
		if !v {
			return "", false
		}
	}
	return "", false
}

func fieldLabel(name, title string) string {
	if strings.TrimSpace(title) != "" {
		return title
	}
	parts := strings.Fields(strings.ReplaceAll(name, "_", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func defaultSchemaJSON() string {
	return `{
  "type": "object",
  "properties": {
    "name": {
      "type": "string"
    },
    "email": {
      "type": "string",
      "format": "email"
    },
    "message": {
      "type": "string"
    }
  },
  "required": ["name", "email", "message"]
}`
}

func defaultUISchemaJSON() string {
	return `{
  "type": "VerticalLayout",
  "elements": [
    {
      "type": "Control",
      "scope": "#/properties/name"
    },
    {
      "type": "Control",
      "scope": "#/properties/email"
    },
    {
      "type": "Control",
      "scope": "#/properties/message",
      "options": {
        "multi": true
      }
    }
  ]
}`
}
