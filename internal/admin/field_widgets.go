// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"
	"github.com/jcsvwinston/nucleus/pkg/storage"

	"github.com/jcsvwinston/orbit/datasource"
)

// The types a form could not hold.
//
// The schema published a widget per field and its vocabulary was scalar:
// text, number, checkbox, date. A model with a JSON column got a text input
// and a person pasting braces into it; a model with an avatar got a text
// input and a person pasting a URL they had to produce somewhere else. Both
// are fields an admin is expected to edit, and neither had anything to edit
// them with.
//
// Two halves, and the second is the one that costs: a widget VOCABULARY the
// schema can name (json, richtext, file, image), and somewhere for a file to
// go — a per-field upload that puts the bytes in the application's storage
// and answers with the key the form writes into the field.
//
// A JSON widget is inferred (a map or a slice is a document, whatever the
// application meant by it); rich text and files are NOT — "this string is
// HTML" and "this string is a storage key" are claims about intent that no
// type carries, so they are declared: PanelConfig.FieldWidgets, keyed
// "Model.Field".
const (
	widgetJSON     = "json"
	widgetRichText = "richtext"
	widgetFile     = "file"
	widgetImage    = "image"
)

// uploadMaxBytes bounds one field upload, and uploadMemoryBytes is the
// in-memory threshold of the multipart parse (the rest spills to temp files).
const (
	uploadMaxBytes    = 32 << 20 // 32 MiB
	uploadMemoryBytes = 8 << 20
)

// fieldWidget resolves the widget a form should render for f: the
// application's declaration first, then what the type itself says.
func (p *Panel) fieldWidget(modelName string, f datasource.FieldInfo) string {
	if declared := p.declaredWidget(modelName, f); declared != "" {
		return declared
	}
	if inferred := inferredWidget(f); inferred != "" {
		return inferred
	}
	return f.HTMLType
}

// declaredWidget reads PanelConfig.FieldWidgets, which accepts the field's Go
// name or its column, compared without regard to case. There is no wildcard:
// an entry names one field.
func (p *Panel) declaredWidget(modelName string, f datasource.FieldInfo) string {
	if len(p.config.FieldWidgets) == 0 {
		return ""
	}
	for _, key := range fieldWidgetKeys(modelName, f) {
		for configured, widget := range p.config.FieldWidgets {
			if strings.EqualFold(configured, key) {
				return normalizeWidget(widget)
			}
		}
	}
	return ""
}

// fieldWidgetKeys are the spellings of a FieldWidgets key that name f, in
// the order declaredWidget tries them: the Go name first, then the column.
// The startup check below resolves an entry through the same list, so what
// it accepts is exactly what the schema will apply.
func fieldWidgetKeys(modelName string, f datasource.FieldInfo) []string {
	return []string{
		modelName + "." + f.Name,
		modelName + "." + f.Column,
		modelName + "." + runtimeColumn(f.Column),
	}
}

// ValidateFieldWidgets refuses, at startup, a field_widgets entry the panel
// could never apply — the rule actions, pages, widgets, branding and the
// locale already follow.
//
// Until it existed, field_widgets was the one declaration nothing checked:
// a widget the panel does not ship ("color" for "colour", "rich-text" for
// "richtext") or a key that names no field of the application (a typo, a
// renamed column, a model that was removed) started normally, and the schema
// published the field as its column type, as if nothing had been declared.
// Each of those entries never had an effect, so refusing it stops nothing
// that worked; it says, once and by name, what the operator would otherwise
// discover field by field in a form.
//
// Two entries that name the same field with different widgets are refused
// too: declaredWidget takes the first spelling that matches, and when both
// spellings are the same one the choice follows map order, so the form could
// change between two requests.
func ValidateFieldWidgets(src datasource.DataSource, widgets map[string]string) error {
	if len(widgets) == 0 {
		return nil
	}
	var models []datasource.ModelInfo
	if src != nil {
		models = src.All()
	}
	return validateFieldWidgets(widgets, models)
}

func validateFieldWidgets(widgets map[string]string, models []datasource.ModelInfo) error {
	keys := make([]string, 0, len(widgets))
	for key := range widgets {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	type claim struct{ key, widget string }
	claimed := map[string]claim{}
	var problems []string
	for _, key := range keys {
		raw := widgets[key]
		widget := normalizeWidget(raw)
		if widget == "" {
			problems = append(problems, fmt.Sprintf("field_widgets[%q]: %q is not a widget the panel draws (json, richtext, file or image)", key, raw))
			continue
		}
		model, field, found := resolveFieldWidgetKey(key, models)
		if !found {
			problems = append(problems, unresolvedFieldWidget(key, models))
			continue
		}
		target := model + "." + field
		if prior, ok := claimed[target]; ok && prior.widget != widget {
			problems = append(problems, fmt.Sprintf("field_widgets[%q] and field_widgets[%q] both name %s, with different widgets (%s, %s)", prior.key, key, target, prior.widget, widget))
			continue
		}
		claimed[target] = claim{key: key, widget: widget}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// resolveFieldWidgetKey finds the model and field a key names, through the
// same spellings declaredWidget reads.
func resolveFieldWidgetKey(key string, models []datasource.ModelInfo) (model, field string, ok bool) {
	for _, mi := range models {
		for _, f := range mi.Fields {
			for _, spelling := range fieldWidgetKeys(mi.Name, f) {
				if strings.EqualFold(key, spelling) {
					return mi.Name, f.Name, true
				}
			}
		}
	}
	return "", "", false
}

// unresolvedFieldWidget says which half of a key names nothing: the model,
// or a field of a model that does exist.
func unresolvedFieldWidget(key string, models []datasource.ModelInfo) string {
	for _, mi := range models {
		prefix := mi.Name + "."
		if len(key) > len(prefix) && strings.EqualFold(key[:len(prefix)], prefix) {
			return fmt.Sprintf("field_widgets[%q]: model %s has no field or column named %q", key, mi.Name, key[len(prefix):])
		}
	}
	if !strings.Contains(key, ".") {
		return fmt.Sprintf("field_widgets[%q]: a key is Model.Field or Model.column", key)
	}
	return fmt.Sprintf("field_widgets[%q]: no model named %q in this application", key, key[:strings.LastIndex(key, ".")])
}

// inferredWidget is what the TYPE says on its own: a map or a slice is a
// document. Nothing else is inferred — a string is a string.
func inferredWidget(f datasource.FieldInfo) string {
	goType := strings.ToLower(strings.TrimSpace(f.GoType))
	switch {
	case strings.HasPrefix(goType, "map["), strings.Contains(goType, "json.rawmessage"):
		return widgetJSON
	case strings.HasPrefix(goType, "[]") && !strings.HasPrefix(goType, "[]byte"):
		return widgetJSON
	case goType == "interface {}", goType == "any":
		return widgetJSON
	}
	return ""
}

func normalizeWidget(widget string) string {
	switch strings.ToLower(strings.TrimSpace(widget)) {
	case widgetJSON:
		return widgetJSON
	case widgetRichText, "rich_text", "html":
		return widgetRichText
	case widgetFile:
		return widgetFile
	case widgetImage:
		return widgetImage
	default:
		return ""
	}
}

// handleFieldUpload takes one file for one field and answers with the key the
// form writes into it. The bytes go to the application's own storage — the
// panel never becomes a file server of its own — and the key is what the
// record ends up holding.
func (p *Panel) handleFieldUpload(c *router.Context) error {
	r := c.Request
	name := c.Param("name")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	// Uploading for a record is writing that record: an operator who may not
	// create or update the model has no reason to put files in its storage.
	if _, err := p.authorizeRecordAction(c, mi, "update"); err != nil {
		if _, createErr := p.authorizeRecordAction(c, mi, "create"); createErr != nil {
			return err
		}
	}
	if mi.ReadOnly {
		return gferrors.Forbidden("model is read-only")
	}
	if p.store == nil {
		return gferrors.BadRequest("storage not configured")
	}

	// ParseMultipartForm's argument is the in-memory threshold, not a limit;
	// MaxBytesReader is the limit, and past it the read fails with 413.
	r.Body = http.MaxBytesReader(c.Writer, r.Body, uploadMaxBytes)
	if err := r.ParseMultipartForm(uploadMemoryBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &gferrors.DomainError{
				Code:       "PAYLOAD_TOO_LARGE",
				Message:    fmt.Sprintf("file too large (max %d MB)", uploadMaxBytes>>20),
				StatusCode: http.StatusRequestEntityTooLarge,
			}
		}
		return gferrors.BadRequest("invalid multipart upload")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return gferrors.BadRequest("file is required")
	}
	defer func() { _ = file.Close() }()

	// The field is optional — a form may upload before it knows where the
	// value goes — but when it is named it has to be a field of this model
	// that holds a file, or the upload is going somewhere nobody asked for.
	fieldKey := strings.TrimSpace(r.FormValue("field"))
	var fieldName string
	if fieldKey != "" {
		_, field, ok := dsResolveField(mi, fieldKey)
		if !ok {
			return gferrors.NotFound("field", fieldKey)
		}
		switch p.fieldWidget(mi.Name, field) {
		case widgetFile, widgetImage:
		default:
			return gferrors.BadRequest(fmt.Sprintf("%s.%s does not hold a file", mi.Name, field.Name))
		}
		fieldName = field.Name
	}

	// The client-supplied filename is untrusted: only its base name reaches
	// the key, and the key is built by the panel.
	filename := filepath.Base(strings.TrimSpace(header.Filename))
	if filename == "." || filename == string(filepath.Separator) || filename == "" {
		filename = "upload"
	}
	key := fmt.Sprintf("admin/uploads/%s/%d_%s", strings.ToLower(mi.Name), time.Now().UTC().UnixNano(), filename)

	info, err := p.store.Put(r.Context(), key, file, storage.PutOptions{
		Visibility:  storage.Private,
		ContentType: header.Header.Get("Content-Type"),
	})
	if err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	p.recordAuditEntry(r, AuditEntry{
		Action:    "field.upload",
		ModelName: mi.Name,
		RecordID:  info.Key,
		NewValue: map[string]any{
			"key":   info.Key,
			"field": fieldName,
			"size":  info.Size,
			"name":  filename,
		},
	})

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"key":          info.Key,
		"field":        fieldName,
		"size":         info.Size,
		"content_type": header.Header.Get("Content-Type"),
		"name":         filename,
	})
}
