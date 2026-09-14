// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package admin

import (
	"fmt"
	"net/http"
	"strings"

	gferrors "github.com/jcsvwinston/nucleus/pkg/errors"
	"github.com/jcsvwinston/nucleus/pkg/router"

	"github.com/jcsvwinston/orbit/datasource"
)

// Relations, as a form needs them.
//
// The schema already marked a foreign key — `is_fk` with the model it points
// at — and that was the whole of it: no endpoint said WHAT it may point at, so
// a form had one honest option left, which was to show the raw id and let the
// person type a number. Everything else about the panel assumed a human knows
// which Author is 47.
//
// Two endpoints answer it, both reading the target model through the same
// Data Studio machinery (so search, tenant confinement and row scope apply
// exactly as they do to a list):
//
//	GET /api/models/{name}/options                     the candidates OF that model
//	GET /api/models/{name}/fields/{field}/options      the candidates the FIELD may point at
//
// Permission is the TARGET's: resolving what an Author id means is reading
// Authors. An operator who may edit Comment and may not list Author gets a
// 403 and a form that still works with raw ids — the panel does not widen a
// grant to make a nicer widget.

// optionsDefaultLimit and optionsMaxLimit bound a lookup page. A relation
// picker is a type-ahead, not a table: the list is meant to be narrowed with
// ?q=, and a bound here is what keeps a model with a million rows from being
// asked for all of them by a form.
const (
	optionsDefaultLimit = 50
	optionsMaxLimit     = 200
)

// relationOption is one candidate a form may point at.
type relationOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// handleFieldOptions resolves what one FIELD may point at: it reads the
// field's foreign model from the schema and answers that model's candidates.
func (p *Panel) handleFieldOptions(c *router.Context) error {
	name := c.Param("name")
	fieldKey := c.Param("field")

	mi, ok := p.src.Get(name)
	if !ok {
		return gferrors.NotFound("model", name)
	}
	_, field, ok := dsResolveField(mi, fieldKey)
	if !ok {
		return gferrors.NotFound("field", fieldKey)
	}
	target := field.ForeignModel
	if target == "" {
		// The field may be declared as a key through the model's foreign-key
		// list rather than on the field itself.
		for _, fk := range mi.ForeignKeys {
			if strings.EqualFold(fk.FieldName, field.Name) || strings.EqualFold(fk.Column, field.Column) {
				target = fk.ForeignModel
				break
			}
		}
	}
	if target == "" {
		return gferrors.BadRequest(fmt.Sprintf("%s.%s is not a foreign key", mi.Name, field.Name))
	}
	return p.writeModelOptions(c, target)
}

// handleModelOptions answers the candidates of one model.
func (p *Panel) handleModelOptions(c *router.Context) error {
	return p.writeModelOptions(c, c.Param("name"))
}

func (p *Panel) writeModelOptions(c *router.Context, modelName string) error {
	r := c.Request
	mi, ok := p.src.Get(modelName)
	if !ok {
		return gferrors.NotFound("model", modelName)
	}
	// Reading the target is what this is, so it is authorized as a read of
	// the target — including the row scope, which confines the candidates to
	// the rows this operator may see.
	rowScope, err := p.authorizeRecordAction(c, mi, "list")
	if err != nil {
		return err
	}

	limit, _, err := parsePositiveQueryInt(r.URL.Query(), "limit")
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = optionsDefaultLimit
	}
	if limit > optionsMaxLimit {
		limit = optionsMaxLimit
	}

	search, err := sanitizeSearchQuery(r.URL.Query().Get("q"))
	if err != nil {
		return err
	}
	// A model with nothing searchable cannot honour ?q=: the backends drop
	// the text and answer every row, which reads as "no match" while showing
	// everything. Say so, the way the list endpoint does.
	if search != "" && !modelSearchable(mi) {
		return gferrors.BadRequest(fmt.Sprintf("search is not available for %s: it has no searchable fields", mi.Name))
	}

	databaseAlias, err := p.requestDatabaseAlias(r)
	if err != nil {
		return gferrors.BadRequest(err.Error())
	}
	if mi.DatabaseAlias != "" && r.URL.Query().Get("db") == "" &&
		r.URL.Query().Get("database") == "" && r.URL.Query().Get("db_alias") == "" {
		databaseAlias = mi.DatabaseAlias
	}
	st, err := p.src.Store(mi.Name, databaseAlias)
	if err != nil {
		return err
	}

	filters := map[string]string{}
	if scope := p.requestTenantScope(r, mi); scope.Enforced() {
		filters[scope.Column()] = scope.Tenant
	}
	if rowScope.Enforced() {
		filters[rowScope.Column()] = rowScope.Owner
	}
	if len(filters) == 0 {
		filters = nil
	}

	page, err := st.List(r.Context(), datasource.Query{
		Page: 1, PageSize: limit, Search: search, Filters: filters,
	})
	if err != nil {
		return err
	}

	labelField, hasLabel := optionLabelField(mi)
	options := make([]relationOption, 0, len(page.Items))
	for _, rec := range page.Items {
		value, ok := canonicalID(recordPKValue(rec, mi))
		if !ok {
			continue
		}
		label := value
		if hasLabel {
			if v, ok := recordValue(rec, labelField); ok {
				if text := strings.TrimSpace(fmt.Sprint(v)); text != "" {
					label = text
				}
			}
		}
		options = append(options, relationOption{Value: value, Label: label})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"model":       mi.Name,
		"value_field": mi.PrimaryKey,
		"label_field": labelField.Name,
		"options":     options,
		// Whether there may be more behind the limit, so a picker can say
		// "keep typing" instead of implying the list is the whole model.
		"truncated": len(page.Items) >= limit,
		"limit":     limit,
	})
}

// optionLabelField picks what a person reads instead of an id: the first
// searchable text field, then the first listed text field, then the first
// text field at all. A model with no text at all has no label and the option
// shows its key, which is still better than a blank row.
func optionLabelField(mi datasource.ModelInfo) (datasource.FieldInfo, bool) {
	var listed, anyText datasource.FieldInfo
	var haveListed, haveAny bool
	for _, f := range mi.Fields {
		if f.IsPK || f.IsExcluded || !isTextField(f) {
			continue
		}
		if f.IsSearch {
			return f, true
		}
		if f.IsList && !haveListed {
			listed, haveListed = f, true
		}
		if !haveAny {
			anyText, haveAny = f, true
		}
	}
	switch {
	case haveListed:
		return listed, true
	case haveAny:
		return anyText, true
	default:
		return datasource.FieldInfo{}, false
	}
}

// isTextField reports whether a field holds something a person can read as a
// label.
func isTextField(f datasource.FieldInfo) bool {
	if strings.Contains(strings.ToLower(f.GoType), "string") {
		return true
	}
	switch strings.ToLower(f.HTMLType) {
	case "text", "email", "url", "textarea":
		return true
	}
	return false
}
