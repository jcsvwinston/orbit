package quarkdatasource

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jcsvwinston/quark"

	"github.com/jcsvwinston/orbit/datasource"
)

// QDAccount is the model the options are measured on: a credential-shaped
// column (PasswordHash), a text column to search, a number, a timestamp and
// a free-text column to hide.
type QDAccount struct {
	ID           int64     `db:"id" pk:"true"`
	Email        string    `db:"email" quark:"not_null"`
	DisplayName  string    `db:"display_name"`
	PasswordHash string    `db:"password_hash" quark:"not_null"`
	Role         string    `db:"role"`
	Score        int64     `db:"score"`
	Notes        string    `db:"notes"`
	CreatedAt    time.Time `db:"created_at"`
}

// QDTenantAccount carries the tenant column, to pin that it cannot be hidden.
type QDTenantAccount struct {
	ID       int64  `db:"id" pk:"true"`
	TenantID string `db:"tenant_id"`
	Email    string `db:"email"`
}

// accountsClient opens an in-memory database with QDAccount migrated.
func accountsClient(t *testing.T) *quark.Client {
	t.Helper()
	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterModel(&QDAccount{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	if err := client.MigrateRegistered(context.Background()); err != nil {
		t.Fatalf("MigrateRegistered: %v", err)
	}
	return client
}

// accounts registers QDAccount with opts on a fresh adapter whose notices go
// to the returned buffer.
func accounts(t *testing.T, opts ...ModelOption) (*Adapter, datasource.RecordStore, context.Context, *bytes.Buffer) {
	t.Helper()
	return accountsOn(t, accountsClient(t), opts...)
}

func accountsOn(t *testing.T, client *quark.Client, opts ...ModelOption) (*Adapter, datasource.RecordStore, context.Context, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	var log bytes.Buffer
	a := New(client, WithLogger(slog.New(slog.NewTextHandler(&log, nil))))
	if err := Register[QDAccount](a, opts...); err != nil {
		t.Fatalf("Register: %v", err)
	}
	st, err := a.Store("QDAccount", "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	return a, st, ctx, &log
}

func field(t *testing.T, a *Adapter, model, name string) datasource.FieldInfo {
	t.Helper()
	mi, ok := a.Get(model)
	if !ok {
		t.Fatalf("%s is not registered", model)
	}
	f, ok := mi.Field(name)
	if !ok {
		t.Fatalf("%s has no field %q", model, name)
	}
	return f
}

// TestOptions_DefaultsAreTheOnesBeforeOptionsExisted pins what a Register
// with no options catalogues: every column listed and filterable, the string
// columns searchable, labels from the Go name — and, new, a column whose
// name looks like a credential left out, said in the log.
func TestOptions_DefaultsAreTheOnesBeforeOptionsExisted(t *testing.T) {
	a, _, _, log := accounts(t)

	for _, col := range []string{"id", "email", "display_name", "role", "score", "notes", "created_at"} {
		f := field(t, a, "QDAccount", col)
		if f.IsExcluded || !f.IsList {
			t.Errorf("%s: excluded=%v list=%v, want shown and listed", col, f.IsExcluded, f.IsList)
		}
	}
	if f := field(t, a, "QDAccount", "email"); !f.IsSearch || !f.IsFilter || f.Label != "Email" {
		t.Errorf("email defaults: %+v", f)
	}
	if f := field(t, a, "QDAccount", "created_at"); f.IsSearch || f.IsFilter {
		t.Errorf("a timestamp is neither searched nor filtered by default: %+v", f)
	}
	if f := field(t, a, "QDAccount", "display_name"); f.Label != "Display Name" {
		t.Errorf("Label = %q, want the humanized Go name", f.Label)
	}

	hash := field(t, a, "QDAccount", "password_hash")
	if !hash.IsExcluded || hash.IsList || hash.IsSearch || hash.IsFilter {
		t.Errorf("password_hash must be excluded by its name, and neither listed, searched nor filtered: %+v", hash)
	}
	notice := log.String()
	if !strings.Contains(notice, "QDAccount.password_hash is left out of Data Studio") || !strings.Contains(notice, `ShowSensitive(`) || !strings.Contains(notice, "column=password_hash") {
		t.Errorf("the exclusion must be said in the log, naming the option that lifts it; got %q", notice)
	}
	if mi, _ := a.Get("QDAccount"); mi.Icon != "" || mi.ReadOnly {
		t.Errorf("no option sets an icon or read-only: %+v", mi)
	}
}

func TestOptions_ShowSensitiveKeepsTheColumnAndSaysNothing(t *testing.T) {
	a, _, _, log := accounts(t, ShowSensitive("password_hash"), ListFields("email", "password_hash"))
	if f := field(t, a, "QDAccount", "password_hash"); f.IsExcluded || !f.IsList {
		t.Errorf("ShowSensitive must keep the column: %+v", f)
	}
	if log.Len() != 0 {
		t.Errorf("nothing was hidden, nothing to say; log = %q", log.String())
	}
}

func TestOptions_DeclareWhatThePanelShows(t *testing.T) {
	a, _, _, _ := accounts(t,
		ListFields("email", "Role", "score"),
		SearchFields("email"),
		Filters("role"),
		Exclude("notes"),
		ReadOnlyFields("email"),
		Label("display_name", "Name"),
		Choices("role", datasource.Choice{Value: "admin", Label: "Administrator"}, datasource.Choice{Value: "member"}),
		Icon("users"),
		OrderBy("score desc, email"),
		PageSize(7),
	)
	mi, _ := a.Get("QDAccount")
	if mi.Icon != "users" {
		t.Errorf("Icon = %q", mi.Icon)
	}
	if mi.ReadOnly {
		t.Error("ReadOnlyFields does not make the model read-only")
	}

	want := map[string]struct {
		list, search, filter, excluded, readOnly bool
		label                                    string
	}{
		"id":            {list: false, search: false, filter: false, label: "ID"},
		"email":         {list: true, search: true, filter: false, readOnly: true, label: "Email"},
		"display_name":  {list: false, search: false, filter: false, label: "Name"},
		"password_hash": {excluded: true, label: "Password Hash"},
		"role":          {list: true, search: false, filter: true, label: "Role"},
		"score":         {list: true, search: false, filter: false, label: "Score"},
		"notes":         {excluded: true, label: "Notes"},
		"created_at":    {list: false, search: false, filter: false, label: "Created At"},
	}
	for col, w := range want {
		f := field(t, a, "QDAccount", col)
		got := struct {
			list, search, filter, excluded, readOnly bool
			label                                    string
		}{f.IsList, f.IsSearch, f.IsFilter, f.IsExcluded, f.IsReadOnly || f.IsPK, f.Label}
		if col == "id" {
			got.readOnly = false
		}
		if got != w {
			t.Errorf("%s = %+v, want %+v", col, got, w)
		}
	}
	role := field(t, a, "QDAccount", "role")
	if role.HTMLType != "select" || len(role.Choices) != 2 || role.Choices[0].Label != "Administrator" || role.Choices[1].Value != "member" {
		t.Errorf("Choices must reach the field and turn its widget into a select: %+v", role)
	}
}

func TestOptions_ReadOnlyModelRefusesEveryWrite(t *testing.T) {
	a, st, ctx, _ := accounts(t, ReadOnly())
	if mi, _ := a.Get("QDAccount"); !mi.ReadOnly {
		t.Fatal("ReadOnly() must catalogue the model read-only")
	}
	if _, err := st.Create(ctx, datasource.Record{"email": "a@x", "password_hash": "h"}); err == nil || !strings.Contains(err.Error(), "registered read-only") {
		t.Errorf("Create: err = %v, want the read-only refusal naming the option", err)
	}
	if err := st.Update(ctx, "1", datasource.Record{"email": "b@x"}); err == nil || !strings.Contains(err.Error(), "registered read-only") {
		t.Errorf("Update: err = %v", err)
	}
	if err := st.Delete(ctx, "1"); err == nil || !strings.Contains(err.Error(), "registered read-only") {
		t.Errorf("Delete: err = %v", err)
	}
	if _, err := st.List(ctx, datasource.Query{}); err != nil {
		t.Errorf("List must still answer: %v", err)
	}
}

// TestOptions_RegisterRefusesWhatItCannotApply: every option that names a
// field the model does not have, contradicts another option, or would
// silently overturn a default is an error at Register, and the model is not
// registered — a misspelled column in ListFields would otherwise list every
// column and look like the default.
func TestOptions_RegisterRefusesWhatItCannotApply(t *testing.T) {
	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterModel(&QDAccount{}, &QDTenantAccount{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}

	for _, tc := range []struct {
		name string
		opts []ModelOption
		want string
	}{
		{"ListFields unknown", []ModelOption{ListFields("emial")}, `ListFields names "emial", which is not a field of the model (fields: id, email`},
		{"SearchFields unknown", []ModelOption{SearchFields("nope")}, `SearchFields names "nope"`},
		{"SearchFields on a number", []ModelOption{SearchFields("score")}, `SearchFields names "score", which is not a text column (int64)`},
		{"Filters on a timestamp", []ModelOption{Filters("created_at")}, `Filters names "created_at", which is of a type the store cannot compare (time.Time)`},
		{"Exclude unknown", []ModelOption{Exclude("secret_sauce")}, `Exclude names "secret_sauce"`},
		{"Exclude the key", []ModelOption{Exclude("id")}, `Exclude names "id", which is the primary key`},
		{"Exclude and list", []ModelOption{ListFields("notes"), Exclude("notes")}, `Exclude names "notes", which is also named in ListFields`},
		{"Exclude and label", []ModelOption{Label("notes", "Memo"), Exclude("notes")}, `Exclude names "notes", which is also named in Label`},
		{"ReadOnlyFields unknown", []ModelOption{ReadOnlyFields("x")}, `ReadOnlyFields names "x"`},
		{"Label unknown", []ModelOption{Label("x", "X")}, `Label names "x"`},
		{"Label empty", []ModelOption{Label("email", " ")}, `Label names "email", which has an empty label`},
		{"Choices unknown", []ModelOption{Choices("x", datasource.Choice{Value: "a"})}, `Choices names "x"`},
		{"Choices empty", []ModelOption{Choices("role")}, `Choices names "role", which has no choices`},
		{"OrderBy unknown", []ModelOption{OrderBy("rank desc")}, `OrderBy names "rank"`},
		{"OrderBy nothing", []ModelOption{OrderBy(" , ")}, `OrderBy(",") names no column`},
		{"OrderBy excluded", []ModelOption{Exclude("notes"), OrderBy("notes")}, `OrderBy names "notes", which is excluded`},
		{"OrderBy sensitive", []ModelOption{OrderBy("password_hash")}, `OrderBy names "password_hash", which is excluded`},
		{"PageSize negative", []ModelOption{PageSize(-1)}, `PageSize(-1) is not a page`},
		{"sensitive listed without ShowSensitive", []ModelOption{ListFields("password_hash")}, `ListFields names "password_hash", which looks like a credential and is excluded by default: name it in ShowSensitive`},
		{"sensitive searched without ShowSensitive", []ModelOption{SearchFields("password_hash")}, `SearchFields names "password_hash", which looks like a credential`},
		{"sensitive labelled without ShowSensitive", []ModelOption{Label("PasswordHash", "Hash")}, `Label names "password_hash", which looks like a credential`},
		{"ShowSensitive on a plain name", []ModelOption{ShowSensitive("notes")}, `ShowSensitive names "notes", which does not look like a credential`},
		{"ShowSensitive unknown", []ModelOption{ShowSensitive("x")}, `ShowSensitive names "x"`},
	} {
		a := New(client)
		err := Register[QDAccount](a, tc.opts...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", tc.name, err, tc.want)
		}
		if !strings.HasPrefix(err.Error(), "quarkdatasource: Register[QDAccount]: ") {
			t.Errorf("%s: the error names the model: %v", tc.name, err)
		}
		if _, ok := a.Get("QDAccount"); ok {
			t.Errorf("%s: a refused Register must not register the model", tc.name)
		}
	}

	a := New(client, WithTenantColumn("tenant_id"))
	err = Register[QDTenantAccount](a, Exclude("tenant_id"))
	if err == nil || !strings.Contains(err.Error(), `Exclude names "tenant_id", which is the tenant column`) {
		t.Errorf("the tenant column cannot be hidden: %v", err)
	}
}

// TestOptions_ExcludedColumnLeavesTheRecords: a hidden column is not in the
// records the store answers, and a write that names it is refused as a
// column the model does not have — the panel's answer to a filter by one.
// A required hidden column is stored as its zero value on a create.
func TestOptions_ExcludedColumnLeavesTheRecords(t *testing.T) {
	_, st, ctx, _ := accounts(t, Exclude("notes"))

	created, err := st.Create(ctx, datasource.Record{"email": "a@x", "role": "admin"})
	if err != nil {
		t.Fatalf("Create without the hidden required column: %v", err)
	}
	for _, hidden := range []string{"notes", "password_hash"} {
		if _, ok := created[hidden]; ok {
			t.Errorf("the created record carries the hidden column %s: %v", hidden, created)
		}
	}
	id := fmtID(created["id"])

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := got["password_hash"]; ok {
		t.Errorf("Get answers the hidden column: %v", got)
	}
	page, err := st.List(ctx, datasource.Query{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("List: %v %+v", err, page)
	}
	if _, ok := page.Items[0]["notes"]; ok {
		t.Errorf("List answers the hidden column: %v", page.Items[0])
	}

	_, err = st.Create(ctx, datasource.Record{"email": "b@x", "password_hash": "smuggled"})
	if problems := fieldProblems(t, err, "create naming a hidden column"); problems["password_hash"] != msgUnknown {
		t.Errorf("problems = %v, want password_hash answered as %q", problems, msgUnknown)
	}
	err = st.Update(ctx, id, datasource.Record{"Notes": "smuggled"})
	if problems := fieldProblems(t, err, "update naming a hidden column by Go name"); problems["Notes"] != msgUnknown {
		t.Errorf("problems = %v", problems)
	}
	if countRows(t, st, ctx) != 1 {
		t.Error("a refused write must write nothing")
	}
}

func TestOptions_SearchLooksOnlyWhereTheModelSays(t *testing.T) {
	_, st, ctx, _ := accounts(t, SearchFields("email"), ShowSensitive("password_hash"))
	if _, err := st.Create(ctx, datasource.Record{"email": "needle@x", "display_name": "haystack", "password_hash": "h"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := st.Create(ctx, datasource.Record{"email": "other@x", "display_name": "needle", "password_hash": "h"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	page, err := st.List(ctx, datasource.Query{Search: "needle"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0]["email"] != "needle@x" {
		t.Errorf("the search must look in email only: %v", page.Items)
	}

	// A hidden string column is not searched either, so hiding one does not
	// make the model unsearchable (the panel refuses a search that would
	// look in an excluded field). The row is written through an adapter that
	// shows the column, and searched through one that hides it.
	client := accountsClient(t)
	_, writer, ctx2, _ := accountsOn(t, client)
	if _, err := writer.Create(ctx2, datasource.Record{"email": "a@x", "notes": "needle"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, hiding, _, _ := accountsOn(t, client, Exclude("notes"))
	page, err = hiding.List(ctx2, datasource.Query{Search: "needle"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 0 {
		t.Errorf("a hidden column must not be searched: %v", page.Items)
	}
	page, err = writer.List(ctx2, datasource.Query{Search: "needle"})
	if err != nil || len(page.Items) != 1 {
		t.Errorf("the adapter that shows the column searches it: %v %v", err, page.Items)
	}
}

func TestOptions_ReadOnlyFieldIsLeftToTheDatabase(t *testing.T) {
	_, st, ctx, _ := accounts(t, ReadOnlyFields("role"))
	created, err := st.Create(ctx, datasource.Record{"email": "a@x", "role": "smuggled"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created["role"] != "" {
		t.Errorf("a read-only field named on a create is left to the database, as the key is: role = %v", created["role"])
	}
	id := fmtID(created["id"])
	if err := st.Update(ctx, id, datasource.Record{"email": "b@x", "role": "smuggled"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := st.Get(ctx, id)
	if got["email"] != "b@x" || got["role"] != "" {
		t.Errorf("the update writes the editable field and leaves the read-only one: %v", got)
	}
}

func TestOptions_PageSizeAndOrderByAreTheDefaultsTheRequestOverrides(t *testing.T) {
	_, st, ctx, _ := accounts(t, PageSize(2), OrderBy("score desc"))
	for i, score := range []int64{10, 30, 20} {
		if _, err := st.Create(ctx, datasource.Record{"email": strings.Repeat("x", i+1) + "@x", "score": score}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	page, err := st.List(ctx, datasource.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.PageSize != 2 || len(page.Items) != 2 || page.TotalPages != 2 {
		t.Errorf("the model's page size applies when the query has none: %+v", page)
	}
	if page.Items[0]["score"].(float64) != 30 || page.Items[1]["score"].(float64) != 20 {
		t.Errorf("the model's order applies when the query has none: %v", page.Items)
	}

	page, err = st.List(ctx, datasource.Query{PageSize: 3, OrderBy: "score asc"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.PageSize != 3 || len(page.Items) != 3 || page.Items[0]["score"].(float64) != 10 {
		t.Errorf("the request's page size and order win: %+v", page)
	}
}

func TestIsSensitiveColumnName(t *testing.T) {
	for name, want := range map[string]bool{
		"password": true, "PasswordHash": true, "password_hash": true, "api_token": true, "ApiKey": true,
		"client_secret": true, "private_key": true, "salt": true, "credential_id": true, "Hash": true,
		"email": false, "token_count": true, "hashtag": false, "notes": false, "": false,
	} {
		if got := IsSensitiveColumnName(name); got != want {
			t.Errorf("IsSensitiveColumnName(%q) = %v, want %v", name, got, want)
		}
	}
}
