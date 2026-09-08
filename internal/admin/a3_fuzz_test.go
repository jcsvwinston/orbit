// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

package admin

// Native fuzz targets for the panel's parsing surfaces (arc A3). Each one
// states a property the surface has to keep, not merely "does not panic":
// a query only ever sorts and filters by columns the panel shows, an id
// crosses the boundary as the same string it came in as, a tenant is never
// trimmed into another one, and no column the validator passed is a column
// the writer cannot take.
//
// The properties are written from the rule the panel means to enforce, not
// from what the code returns. That is not a stylistic preference: the first
// version of FuzzDataStudioQuery built its allow-list out of every field of
// the model, which is the sanitiser's own codomain, so it blessed the one
// thing this surface got wrong (order_by=password_hash) and could never have
// reported it.
//
// The seed corpora are the inputs of the tests already in this package, the
// shapes that broke in the 2026-09 maturity audit (OR-14 ids, the TENANT_ID
// spelling that slipped past an exact-match guard, the "order_by=drop table
// users" of TestPanel), and the hidden-column queries the rewrite found.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jcsvwinston/orbit/datasource"
)

// fuzzModelInfo is a model with one field of every shape the parsing surfaces
// branch on: a synthetic pk column ("i_d" → "id"), a bool filter, a tenant
// field, a read-only field, an excluded one, and an excluded one that is
// still tagged listable and filterable — the shape the Field settings editor
// produces when an operator hides a column that was already in the list, and
// the one that decides whether "excluded" is enforced or merely obeyed by
// well-behaved clients.
func fuzzModelInfo() datasource.ModelInfo {
	return datasource.ModelInfo{
		Name:       "AdminUser",
		Plural:     "AdminUsers",
		Table:      "admin_users",
		PrimaryKey: "ID",
		Fields: []datasource.FieldInfo{
			{Name: "ID", Column: "i_d", GoType: "uint", IsPK: true, IsList: true, IsFilter: true},
			{Name: "Email", Column: "email", GoType: "string", IsRequired: true, IsList: true, IsSearch: true, IsFilter: true},
			{Name: "Name", Column: "name", GoType: "string", IsList: true, IsSearch: true, IsFilter: true},
			{Name: "Age", Column: "age", GoType: "int", IsList: true, IsFilter: true},
			{Name: "Balance", Column: "balance", GoType: "float64", IsList: true},
			{Name: "IsActive", Column: "is_active", GoType: "bool", IsList: true, IsFilter: true},
			{Name: "TenantID", Column: "tenant_id", GoType: "string", IsTenantField: true, IsFilter: true},
			{Name: "CreatedAt", Column: "created_at", GoType: "time.Time", IsList: true, IsReadOnly: true},
			{Name: "PasswordHash", Column: "password_hash", GoType: "string", IsExcluded: true},
			{Name: "SecretToken", Column: "secret_token", GoType: "string", IsList: true, IsFilter: true, IsExcluded: true},
		},
		TenantField: "tenant_id",
	}
}

// fuzzVisibleColumns is the allow-list the query surfaces may draw from,
// derived from the panel's own visibility rule rather than from what the code
// happens to accept: a column the panel would show, plus the synthetic "id".
//
// IsExcluded is the rule (hardening.go: "the model marked them as never shown
// in Data Studio" — handleGetSchema drops the field, the exporters skip the
// column, redactAuditValues masks it). IsReadOnly deliberately is NOT: a
// read-only created_at is rendered in the list and is the column operators
// sort by most, so barring it here would fit the property to a restriction the
// panel does not mean to have.
func fuzzVisibleColumns(mi datasource.ModelInfo) map[string]bool {
	allowed := map[string]bool{"id": true}
	for _, f := range mi.Fields {
		if f.IsExcluded {
			continue
		}
		allowed[runtimeColumn(f.Column)] = true
	}
	return allowed
}

// fuzzExcludedColumns is the complement: the spellings by which a request
// could name a hidden column (runtime column, storage column, Go name).
func fuzzExcludedColumns(mi datasource.ModelInfo) map[string]bool {
	hidden := map[string]bool{}
	for _, f := range mi.Fields {
		if !f.IsExcluded {
			continue
		}
		for _, key := range []string{runtimeColumn(f.Column), f.Column, f.Name} {
			if key != "" {
				hidden[strings.ToLower(key)] = true
			}
		}
	}
	return hidden
}

// FuzzDataStudioQuery drives the list endpoint's query parsing — the whole of
// it, the way the handler calls it: order_by, filters, search and pagination
// off one untrusted query string.
//
// The property that matters is the order-by one, and it is stated against the
// panel's visibility rule, not against the sanitiser's codomain: what comes
// out is built exclusively from the columns the panel would show and the two
// directions, so no byte of the request reaches the ORDER BY clause the store
// interpolates AND no request sorts by a column the panel hides. The second
// half is what found the defect this PR fixes — order_by=password_hash was
// accepted and handed to the store, which paginates the table in hash order:
// a comparison oracle over a value the schema endpoint, the exporters and the
// audit log all take care never to show. The filter map is bounded the same
// way (keys are visible runtime columns; a bool filter is normalised to
// "1"/"0" and never carries the request's spelling).
func FuzzDataStudioQuery(f *testing.F) {
	for _, seed := range []string{
		"order_by=name+asc",                       // panel_test.go
		"order_by=drop+table+users",               // panel_test.go: the rejected one
		"order_by=id+desc%2Cname+asc",             // multi-clause
		"order_by=NAME+DESC",                      // resolution is case-insensitive
		"order_by=email+asc%3B+--",                // a comment tail
		"page=1&page_size=25&search=%25",          // a bare LIKE wildcard in the search
		"is_active=yes&order_by=id",               // bool normalisation
		"TENANT_ID=other&tenant=acme",             // the spelling that slipped past an exact guard
		"order_by=password_hash+desc",             // a column the panel never shows
		"order_by=PasswordHash",                   // the same column, Go-name spelling
		"password_hash=x",                         // the same asymmetry on the filter side
		"secret_token=x&order_by=secret_token",    // hidden, yet tagged filterable and listable
		"unknown_column=1",                        // must be a 400, never a filter
		"page=-3&page_size=abc",                   // lenient pagination
		"search=" + strings.Repeat("x", 300),      // over the 256-character bound
		"order_by=+%2C+%2C+&db=main&database=alt", // empty clauses and reserved keys
	} {
		f.Add(seed)
	}

	mi := fuzzModelInfo()
	allowed := fuzzVisibleColumns(mi)
	hidden := fuzzExcludedColumns(mi)

	f.Fuzz(func(t *testing.T, rawQuery string) {
		// The handler reads r.URL.Query(), which keeps whatever parsed and
		// drops the rest; parse the same way so the target sees what the
		// handler sees.
		values, _ := url.ParseQuery(rawQuery)

		clause, err := dsSanitizeOrderBy(mi, values.Get("order_by"))
		if err != nil {
			if clause != "" {
				t.Fatalf("rejected order_by returned a clause: %q", clause)
			}
		} else if clause != "" {
			for _, part := range strings.Split(clause, ", ") {
				fields := strings.Split(part, " ")
				if len(fields) != 2 {
					t.Fatalf("order_by %q produced %q: clause %q is not \"column direction\"",
						values.Get("order_by"), clause, part)
				}
				if hidden[strings.ToLower(fields[0])] {
					t.Fatalf("order_by %q sorts by %q, a column %s never shows: the clause is an oracle over it",
						values.Get("order_by"), fields[0], mi.Name)
				}
				if !allowed[fields[0]] {
					t.Fatalf("order_by %q produced column %q, which is not a visible column of %s",
						values.Get("order_by"), fields[0], mi.Name)
				}
				if fields[1] != "asc" && fields[1] != "desc" {
					t.Fatalf("order_by %q produced direction %q", values.Get("order_by"), fields[1])
				}
			}
			// Round trip: the clause is itself a valid order_by and
			// re-validating it changes nothing, so what the store sees is a
			// fixed point of the sanitiser.
			again, err := dsSanitizeOrderBy(mi, clause)
			if err != nil || again != clause {
				t.Fatalf("order_by %q produced %q, which re-sanitises to (%q, %v)",
					values.Get("order_by"), clause, again, err)
			}
		}

		filters, err := dsCollectFilters(mi, values)
		if err == nil {
			for col, val := range filters {
				if hidden[strings.ToLower(col)] {
					t.Fatalf("filter on %q, a column %s never shows (query %q)", col, mi.Name, rawQuery)
				}
				if !allowed[col] {
					t.Fatalf("filter column %q is not a visible column of %s (query %q)", col, mi.Name, rawQuery)
				}
				if col == "is_active" && val != "0" && val != "1" {
					t.Fatalf("bool filter kept the request spelling %q (query %q)", val, rawQuery)
				}
			}
			for _, reserved := range []string{"page", "page_size", "search", "order_by", "db", "database", "db_alias", "tenant"} {
				if _, ok := filters[reserved]; ok {
					t.Fatalf("reserved parameter %q became a filter (query %q)", reserved, rawQuery)
				}
			}
		}

		if search, err := sanitizeSearchQuery(values.Get("search")); err == nil {
			if len(search) > 256 {
				t.Fatalf("search of %d characters passed the 256 bound", len(search))
			}
			if search != strings.TrimSpace(values.Get("search")) {
				t.Fatalf("search %q was rewritten to %q", values.Get("search"), search)
			}
		}

		for _, key := range []string{"page", "page_size"} {
			n, provided, err := parsePositiveQueryInt(values, key)
			if err != nil {
				continue
			}
			if provided && n < 1 {
				t.Fatalf("%s=%q parsed to %d, which is not a page number", key, values.Get(key), n)
			}
			if !provided && n != 0 {
				t.Fatalf("%s absent parsed to %d", key, n)
			}
		}
	})
}

// FuzzRecordIDBoundary drives the two spellings that cross the API boundary:
// a record id (ADR-001 D1, the surface of OR-14 — the bulk endpoint decoded
// ids as []uint and answered 400 for {"ids":["abc"]}) and a tenant value,
// which is compared verbatim because both adapters store it verbatim.
func FuzzRecordIDBoundary(f *testing.F) {
	seeds := []struct {
		ids    string
		tenant string
		num    float64
	}{
		{`["7"]`, "acme", 3},
		{`[7]`, " acme ", 3.5}, // A1: a padded tenant is a tenant of its own
		{`["abc", 8, " 0b1c2d3e-0000-4000-8000-000000000001 "]`, "", 1e15}, // OR-14 seeds
		{`["7", -1, 1.5]`, "acme\n", -5},
		{`[null]`, " ", math.NaN()},
		{`[{}]`, "0", math.Inf(1)},
		{`[]`, "TENANT", 0},
		{`["   "]`, "acme", 9.007199254740993e15},
	}
	for _, s := range seeds {
		f.Add(s.ids, s.tenant, s.num)
	}

	f.Fuzz(func(t *testing.T, idsJSON, tenant string, num float64) {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(idsJSON), &raw); err == nil {
			if ids, err := decodeRecordIDs(raw); err == nil {
				for i, id := range ids {
					if id == "" || id != strings.TrimSpace(id) {
						t.Fatalf("ids[%d] = %q: an accepted id is non-empty and trimmed", i, id)
					}
					// The boundary string and the canonical form of a record
					// key are the same spelling; the panel compares one with
					// the other when it confirms a row.
					if got, ok := canonicalID(id); !ok || got != id {
						t.Fatalf("ids[%d] = %q canonicalises to (%q, %v)", i, id, got, ok)
					}
				}
				// Round trip: an accepted id, sent back as a JSON string,
				// decodes to itself — so a number and its string spelling
				// name the same row on the next request.
				again := make([]json.RawMessage, len(ids))
				for i, id := range ids {
					b, err := json.Marshal(id)
					if err != nil {
						t.Fatalf("marshal id %q: %v", id, err)
					}
					again[i] = b
				}
				out, err := decodeRecordIDs(again)
				if err != nil {
					t.Fatalf("re-decoding %v: %v", ids, err)
				}
				if strings.Join(out, "\x00") != strings.Join(ids, "\x00") {
					t.Fatalf("ids round trip: %v became %v", ids, out)
				}
			}
		}

		// A tenant is never trimmed: ' acme ' is a tenant of its own, one no
		// request resolves to, and a guard that compared it trimmed let a
		// scoped operator move rows into it.
		got, ok := canonicalTenant(tenant)
		if got != tenant || ok != (tenant != "") {
			t.Fatalf("canonicalTenant(%q) = (%q, %v)", tenant, got, ok)
		}
		if b, okb := canonicalTenant([]byte(tenant)); b != got || okb != ok {
			t.Fatalf("canonicalTenant([]byte(%q)) = (%q, %v), want (%q, %v)", tenant, b, okb, got, ok)
		}
		if trimmed := strings.TrimSpace(tenant); trimmed != tenant && tenant != "" {
			if other, _ := canonicalTenant(trimmed); other == got {
				t.Fatalf("canonicalTenant collapsed the padded %q onto %q", tenant, trimmed)
			}
		}
		// The id spelling of the same scalar does trim, and only that.
		if id, okID := canonicalID(tenant); id != strings.TrimSpace(tenant) || okID != (strings.TrimSpace(tenant) != "") {
			t.Fatalf("canonicalID(%q) = (%q, %v)", tenant, id, okID)
		}

		// A numeric key arrives as float64 from the JSON record: rendering it
		// must not change the value it names.
		num64, okNum := canonicalID(num)
		switch {
		case math.IsNaN(num) || math.IsInf(num, 0):
			if okNum {
				t.Fatalf("canonicalID(%v) = (%q, true): it names no row", num, num64)
			}
		default:
			if !okNum {
				t.Fatalf("canonicalID(%v) returned no id", num)
			}
			back, err := strconv.ParseFloat(num64, 64)
			if err != nil || back != num {
				t.Fatalf("canonicalID(%v) = %q, which reads back as (%v, %v)", num, num64, back, err)
			}
		}
	})
}

// FuzzImportValidation drives the CSV import path: parse, then validate. The
// property is the one ImportFromFile depends on — it aborts on any validation
// error and hands the records straight to Create otherwise, so a record the
// validator passed must carry only columns the writer can take, with values
// of the declared type. The type checks are re-derived here with strconv
// rather than by calling validateFieldValue, so the target does not agree
// with the code by construction.
func FuzzImportValidation(f *testing.F) {
	for _, seed := range []string{
		"email,name,age\nfoo@example.com,Foo,33\n",
		"email,name\nfoo@example.com,\"Foo, Jr\"\n",
		"i_d,email\n7,foo@example.com\n",
		"id,email\n7,foo@example.com\n",            // the header the pk lookup had to learn
		"TENANT_ID,email\nother,foo@example.com\n", // the spelling that slipped past an exact guard
		"password_hash,email\nx,foo@example.com\n", // excluded column
		"created_at,email\n2026-09-08,a@b.c\n",     // read-only column
		"age,is_active\nnot-a-number,maybe\n",
		"age,is_active\n9223372036854775808,1\n", // out of range for int64
		"_model,email\nOther,a@b.c\n",
		" email , name \n a@b.c , Foo \n", // padding around headers and values
		"email\n",                         // header only
		"",
	} {
		f.Add([]byte(seed))
	}

	mi := fuzzModelInfo()

	f.Fuzz(func(t *testing.T, data []byte) {
		records, err := ParseImportData(bytes.NewReader(data), "csv")
		if err != nil {
			if records != nil {
				t.Fatalf("a rejected CSV returned %d records", len(records))
			}
			return
		}

		var headers []string
		for i, rec := range records {
			for key, val := range rec {
				if key == "" || key != strings.TrimSpace(key) {
					t.Fatalf("record %d carries the key %q: a header reaches the writer trimmed and non-empty", i, key)
				}
				s, isString := val.(string)
				if !isString {
					t.Fatalf("record %d key %q holds %T: a CSV cell is text", i, key, val)
				}
				if s != strings.TrimSpace(s) {
					t.Fatalf("record %d key %q holds the untrimmed %q", i, key, s)
				}
			}
			// A CSV is rectangular: every row carries the same header set.
			keys := recordKeys(rec)
			if i == 0 {
				headers = keys
			} else if strings.Join(keys, "\x00") != strings.Join(headers, "\x00") {
				t.Fatalf("record %d has keys %v, record 0 has %v", i, keys, headers)
			}
		}

		flagged := map[string]bool{}
		flaggedRow := map[int]bool{}
		for _, e := range ValidateImportData(mi, records, "") {
			if e.Row < 0 || e.Row >= len(records) {
				t.Fatalf("validation reported row %d of %d records", e.Row, len(records))
			}
			if e.Field == "" {
				// A row-level refusal (a record of another model) stops the
				// validator before it reaches the row's columns, and aborts
				// the whole import: the row is out, columns and all.
				flaggedRow[e.Row] = true
				continue
			}
			flagged[fmt.Sprintf("%d\x00%s", e.Row, e.Field)] = true
		}
		for i, rec := range records {
			if flaggedRow[i] {
				continue
			}
			for key, val := range rec {
				if key == "_model" {
					continue
				}
				if flagged[fmt.Sprintf("%d\x00%s", i, key)] {
					continue
				}
				field := dsFindFieldByColumn(mi, key)
				if field == nil {
					t.Fatalf("record %d: unknown column %q passed validation and would reach Create", i, key)
				}
				if field.IsReadOnly || field.IsExcluded {
					t.Fatalf("record %d: column %q is read-only or excluded and passed validation", i, key)
				}
				s, _ := val.(string)
				if s == "" {
					continue
				}
				if err := typedValueError(*field, s); err != nil {
					t.Fatalf("record %d: %v passed validation", i, err)
				}
			}
		}
	})
}

// recordKeys is the sorted key set of one parsed row.
func recordKeys(rec map[string]interface{}) []string {
	keys := make([]string, 0, len(rec))
	for k := range rec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// typedValueError re-derives, from the declared Go type alone, whether a cell
// is of that type. It is the independent reading of what validateFieldValue
// promises the writer.
func typedValueError(field datasource.FieldInfo, value string) error {
	switch field.GoType {
	case "int", "int8", "int16", "int32", "int64":
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("column %q holds %q, which is not an integer", field.Column, value)
		}
	case "uint", "uint8", "uint16", "uint32", "uint64":
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("column %q holds %q, which is not an unsigned integer", field.Column, value)
		}
	case "float32", "float64":
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("column %q holds %q, which is not a number", field.Column, value)
		}
	case "bool":
		switch strings.ToLower(value) {
		case "true", "false", "1", "0":
		default:
			return fmt.Errorf("column %q holds %q, which is not a boolean", field.Column, value)
		}
	}
	return nil
}

// TestDataStudioQuery_AColumnThePanelHidesIsNotSortable is the named form of
// the property FuzzDataStudioQuery found: the rule "IsExcluded means never
// shown in Data Studio" is enforced by the query surfaces, not merely obeyed
// by the SPA (which never learns the column exists, because handleGetSchema
// drops it). A hidden column is not a sort key and not a filter key; every
// other column, read-only ones included, still is.
func TestDataStudioQuery_AColumnThePanelHidesIsNotSortable(t *testing.T) {
	mi := fuzzModelInfo()

	for _, key := range []string{"password_hash", "PasswordHash", "PASSWORD_HASH", "secret_token", "SecretToken"} {
		if clause, err := dsSanitizeOrderBy(mi, key+" desc"); err == nil {
			t.Errorf("order_by=%q was accepted as %q: sorting by a hidden column is a comparison oracle over it", key, clause)
		}
		if _, _, err := dsNormalizeFilter(mi, key, "x"); err == nil {
			t.Errorf("filter %s=x was accepted: filtering by a hidden column is the same oracle", key)
		}
	}

	// The columns the panel does show stay sortable — including the
	// read-only one, which is the column operators sort by most.
	for _, key := range []string{"id", "email", "name", "created_at", "CreatedAt", "is_active"} {
		if _, err := dsSanitizeOrderBy(mi, key+" desc"); err != nil {
			t.Errorf("order_by=%q was refused: %v", key, err)
		}
	}
}
