package quarkdatasource

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jcsvwinston/orbit/datasource"
)

// ModelOption declares, at Register, what Data Studio does with a model: the
// columns it lists, searches and filters, the ones it never shows, the labels
// and choices a form draws, and the model's icon, default order and page
// size. Quark itself carries none of this — it is an ORM, and a struct tag
// that said "list" would be a UI decision in the data layer — so it is said
// here, in Go, next to the registration.
//
// The options are the ones the Nucleus adapter reads from model.ModelConfig
// (ListFields, SearchFields, Filters, ExcludeFields, FieldLabels, Icon,
// ReadOnly, OrderBy, PageSize) and from the admin struct tag (readonly,
// choices), so a model moved from pkg/model to Quark declares the same things
// in the same words, and the panel over it is the same panel.
//
// A field is named by its storage column or its Go name, case-insensitively,
// as the panel resolves it. A name that matches no field is an error from
// Register rather than an entry that silently does nothing: ListFields with a
// misspelled column would list every column and look like a default.
type ModelOption func(*modelConfig)

type modelConfig struct {
	listFields     []string
	searchFields   []string
	filters        []string
	exclude        []string
	readOnlyFields []string
	showSensitive  []string
	labels         map[string]string
	choices        map[string][]datasource.Choice
	icon           string
	orderBy        string
	pageSize       int
	readOnly       bool
}

// ListFields names the columns the list view shows. Nothing named keeps the
// default: every column.
func ListFields(fields ...string) ModelOption {
	return func(c *modelConfig) { c.listFields = append(c.listFields, fields...) }
}

// SearchFields names the text columns ?search= looks in. Nothing named keeps
// the default: every string column. A column that is not text is refused.
func SearchFields(fields ...string) ModelOption {
	return func(c *modelConfig) { c.searchFields = append(c.searchFields, fields...) }
}

// Filters names the columns the list offers as filters. Nothing named keeps
// the default: every scalar column. A column of a kind the store cannot
// compare (a struct, a slice) is refused.
func Filters(fields ...string) ModelOption {
	return func(c *modelConfig) { c.filters = append(c.filters, fields...) }
}

// Exclude names the columns Data Studio never shows: not in the schema, the
// list, a form, an export or the audit trail, and refused — as a column the
// model does not have — when a filter, a sort, a search or a write names
// them. The primary key and the tenant column cannot be excluded. A required
// column that is excluded is stored as its Go zero value on a create, as on
// the Nucleus adapter: hide a column the application fills itself.
func Exclude(fields ...string) ModelOption {
	return func(c *modelConfig) { c.exclude = append(c.exclude, fields...) }
}

// ReadOnly lists the model and refuses every write to it (create, update,
// delete), whatever its key. A model with a composite primary key, or none,
// is read-only without being told.
func ReadOnly() ModelOption {
	return func(c *modelConfig) { c.readOnly = true }
}

// ReadOnlyFields names the columns a form shows and does not edit. A write
// that names one leaves it to the database, as it leaves the primary key.
func ReadOnlyFields(fields ...string) ModelOption {
	return func(c *modelConfig) { c.readOnlyFields = append(c.readOnlyFields, fields...) }
}

// Label sets the label the panel shows for one field, instead of the one
// derived from its Go name ("CreatedAt" → "Created At").
func Label(field, label string) ModelOption {
	return func(c *modelConfig) {
		if c.labels == nil {
			c.labels = make(map[string]string)
		}
		c.labels[field] = label
	}
}

// Choices gives one field a closed set of values: the form draws a select
// over them instead of a free input.
func Choices(field string, choices ...datasource.Choice) ModelOption {
	return func(c *modelConfig) {
		if c.choices == nil {
			c.choices = make(map[string][]datasource.Choice)
		}
		c.choices[field] = append(c.choices[field], choices...)
	}
}

// Icon names the icon the panel's sidebar shows for the model.
func Icon(icon string) ModelOption {
	return func(c *modelConfig) { c.icon = strings.TrimSpace(icon) }
}

// OrderBy is the order a list has when the request asks for none, in the
// panel's own form: "created_at desc", "status, created_at desc". Every
// column named must be one of the model's and not excluded.
func OrderBy(orderBy string) ModelOption {
	return func(c *modelConfig) { c.orderBy = strings.TrimSpace(orderBy) }
}

// PageSize is the page a list has when the request asks for none; the
// adapter's default is 25.
func PageSize(n int) ModelOption {
	return func(c *modelConfig) { c.pageSize = n }
}

// ShowSensitive keeps the named columns in Data Studio although their name
// looks like a credential (see IsSensitiveColumnName), which excludes a
// column by default. Naming such a column in ListFields, SearchFields,
// Filters, ReadOnlyFields, Label or Choices without naming it here is an
// error: the default and the option would disagree in silence.
func ShowSensitive(fields ...string) ModelOption {
	return func(c *modelConfig) { c.showSensitive = append(c.showSensitive, fields...) }
}

// IsSensitiveColumnName reports whether a column or field name looks like a
// credential: it contains password, secret, token, credential, api_key,
// apikey or private_key, or ends in hash or salt, case-insensitively. A
// column with such a name is excluded from Data Studio unless Register is
// told ShowSensitive; the rule is the one the panel's audit trail redacts
// by, so the two agree on what a credential looks like.
func IsSensitiveColumnName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, needle := range []string{"password", "secret", "token", "credential", "api_key", "apikey", "private_key"} {
		if strings.Contains(n, needle) {
			return true
		}
	}
	return strings.HasSuffix(n, "hash") || strings.HasSuffix(n, "salt")
}

// optionError is what Register answers for an option it cannot apply: it
// names the model, the option and the field, so a wiring mistake reads as
// one at boot and not as a column that quietly went missing from the panel.
func optionError(model, option, field, why string) error {
	return fmt.Errorf("quarkdatasource: Register[%s]: %s names %q, which %s", model, option, field, why)
}

// applyModelOptions rewrites info — built with the adapter's defaults — with
// what the options declare, and returns the columns it excluded on its own
// because of their name, for Register to say so in the log. A name that
// resolves to no field, an option that contradicts another, or a default the
// option would silently overturn is an error, and info is left as it was.
func applyModelOptions(info *datasource.ModelInfo, cfg modelConfig) ([]string, error) {
	out := *info
	out.Fields = append([]datasource.FieldInfo(nil), info.Fields...)
	model := info.Name

	// resolve maps an option's field name to its index in out.Fields, by
	// column or Go name, as the panel resolves a key.
	resolve := func(option, name string) (int, error) {
		for i, f := range out.Fields {
			if strings.EqualFold(name, f.Column) || strings.EqualFold(name, f.Name) {
				return i, nil
			}
		}
		return 0, optionError(model, option, name, "is not a field of the model (fields: "+columnList(out.Fields)+")")
	}
	// named records which option first named each field, to refuse a field
	// that is both excluded and asked for.
	named := make(map[int]string)
	collect := func(option string, fields []string) (map[int]bool, error) {
		set := make(map[int]bool, len(fields))
		for _, name := range fields {
			i, err := resolve(option, name)
			if err != nil {
				return nil, err
			}
			set[i] = true
			if _, seen := named[i]; !seen {
				named[i] = option
			}
		}
		return set, nil
	}

	list, err := collect("ListFields", cfg.listFields)
	if err != nil {
		return nil, err
	}
	search, err := collect("SearchFields", cfg.searchFields)
	if err != nil {
		return nil, err
	}
	for i := range search {
		if !out.Fields[i].IsSearch {
			return nil, optionError(model, "SearchFields", out.Fields[i].Column, "is not a text column ("+out.Fields[i].GoType+"): a search looks in text")
		}
	}
	filters, err := collect("Filters", cfg.filters)
	if err != nil {
		return nil, err
	}
	for i := range filters {
		if !out.Fields[i].IsFilter {
			return nil, optionError(model, "Filters", out.Fields[i].Column, "is of a type the store cannot compare ("+out.Fields[i].GoType+")")
		}
	}
	readOnly, err := collect("ReadOnlyFields", cfg.readOnlyFields)
	if err != nil {
		return nil, err
	}
	labelKeys := make([]string, 0, len(cfg.labels))
	for name := range cfg.labels {
		labelKeys = append(labelKeys, name)
	}
	sort.Strings(labelKeys)
	labels := make(map[int]string, len(cfg.labels))
	for _, name := range labelKeys {
		i, err := resolve("Label", name)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(cfg.labels[name]) == "" {
			return nil, optionError(model, "Label", name, "has an empty label")
		}
		labels[i] = cfg.labels[name]
		if _, seen := named[i]; !seen {
			named[i] = "Label"
		}
	}
	choiceKeys := make([]string, 0, len(cfg.choices))
	for name := range cfg.choices {
		choiceKeys = append(choiceKeys, name)
	}
	sort.Strings(choiceKeys)
	choices := make(map[int][]datasource.Choice, len(cfg.choices))
	for _, name := range choiceKeys {
		i, err := resolve("Choices", name)
		if err != nil {
			return nil, err
		}
		if len(cfg.choices[name]) == 0 {
			return nil, optionError(model, "Choices", name, "has no choices")
		}
		choices[i] = append([]datasource.Choice(nil), cfg.choices[name]...)
		if _, seen := named[i]; !seen {
			named[i] = "Choices"
		}
	}
	show := make(map[int]bool, len(cfg.showSensitive))
	for _, name := range cfg.showSensitive {
		i, err := resolve("ShowSensitive", name)
		if err != nil {
			return nil, err
		}
		if !IsSensitiveColumnName(out.Fields[i].Column) && !IsSensitiveColumnName(out.Fields[i].Name) {
			return nil, optionError(model, "ShowSensitive", name, "does not look like a credential: nothing hides it")
		}
		show[i] = true
	}

	// The explicit exclusions, then the ones the name decides.
	exclude := make(map[int]bool, len(cfg.exclude))
	for _, name := range cfg.exclude {
		i, err := resolve("Exclude", name)
		if err != nil {
			return nil, err
		}
		f := out.Fields[i]
		switch {
		case f.IsPK:
			return nil, optionError(model, "Exclude", name, "is the primary key: a row whose key is hidden cannot be opened")
		case f.IsTenantField:
			return nil, optionError(model, "Exclude", name, "is the tenant column: the panel confines operators by it")
		}
		if by, asked := named[i]; asked {
			return nil, optionError(model, "Exclude", name, "is also named in "+by)
		}
		exclude[i] = true
	}
	var hidden []string
	for i, f := range out.Fields {
		if exclude[i] || show[i] || f.IsPK || f.IsTenantField {
			continue
		}
		if !IsSensitiveColumnName(f.Column) && !IsSensitiveColumnName(f.Name) {
			continue
		}
		if by, asked := named[i]; asked {
			return nil, optionError(model, by, f.Column, "looks like a credential and is excluded by default: name it in ShowSensitive to show it")
		}
		exclude[i] = true
		hidden = append(hidden, f.Column)
	}

	if cfg.pageSize < 0 {
		return nil, fmt.Errorf("quarkdatasource: Register[%s]: PageSize(%d) is not a page", model, cfg.pageSize)
	}
	if cfg.orderBy != "" {
		clauses := parseOrderBy(cfg.orderBy)
		if len(clauses) == 0 {
			return nil, fmt.Errorf("quarkdatasource: Register[%s]: OrderBy(%q) names no column", model, cfg.orderBy)
		}
		for _, clause := range clauses {
			i, err := resolve("OrderBy", clause.column)
			if err != nil {
				return nil, err
			}
			if exclude[i] {
				return nil, optionError(model, "OrderBy", clause.column, "is excluded: a sort by a hidden column pages the rows in its order")
			}
		}
	}

	for i := range out.Fields {
		f := &out.Fields[i]
		if len(list) > 0 {
			f.IsList = list[i]
		}
		if len(search) > 0 {
			f.IsSearch = search[i]
		}
		if len(filters) > 0 {
			f.IsFilter = filters[i]
		}
		if readOnly[i] {
			f.IsReadOnly = true
		}
		if label, ok := labels[i]; ok {
			f.Label = label
		}
		if c, ok := choices[i]; ok {
			f.Choices = c
			f.HTMLType = "select"
		}
		if exclude[i] {
			// A hidden column is not listed, searched or filtered either:
			// the panel refuses a search that would look in an excluded
			// field, for every operator, so a searchable one that stayed
			// marked would make the whole model unsearchable.
			f.IsExcluded = true
			f.IsList, f.IsSearch, f.IsFilter = false, false, false
		}
	}
	if cfg.icon != "" {
		out.Icon = cfg.icon
	}
	if cfg.readOnly {
		out.ReadOnly = true
	}
	*info = out
	return hidden, nil
}

func columnList(fields []datasource.FieldInfo) string {
	cols := make([]string, 0, len(fields))
	for _, f := range fields {
		cols = append(cols, f.Column)
	}
	return strings.Join(cols, ", ")
}
