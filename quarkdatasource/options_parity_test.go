package quarkdatasource

import (
	"testing"

	"github.com/jcsvwinston/nucleus/pkg/model"
	"github.com/jcsvwinston/quark"
)

// NMAccount is QDAccount as pkg/model reads it: the same fields, the same Go
// names, no tags — the two data layers read `db:"…"` differently, so the
// Quark model cannot be handed to the Nucleus registry as it is. What the
// test compares is what the two declare about the SAME fields.
type NMAccount struct {
	ID           int64
	Email        string
	DisplayName  string
	PasswordHash string
	Role         string
	Score        int64
	Notes        string
}

// TestOptions_ParityWithTheNucleusRegistry pins that a model moved from
// pkg/model to Quark declares the same things in the same words, and that
// the panel sees the same ModelInfo for it: the Nucleus adapter copies these
// flags field by field from model.FieldMeta (datasource/nucleus toFieldInfo),
// so comparing against the registry is comparing against that adapter
// without the root module in this module's graph (ADR-012). One default
// differs on purpose and is pinned here: a credential-shaped column is
// excluded by name on the Quark side, and by ExcludeFields on the Nucleus
// side — the Nucleus registry hides nothing on its own.
func TestOptions_ParityWithTheNucleusRegistry(t *testing.T) {
	reg := model.NewRegistry()
	if err := reg.Register(&NMAccount{}, model.ModelConfig{
		Icon:          "users",
		ListFields:    []string{"Email", "Role", "Score"},
		SearchFields:  []string{"Email"},
		Filters:       []string{"Role"},
		ExcludeFields: []string{"Notes", "PasswordHash"},
		FieldLabels:   map[string]string{"DisplayName": "Name"},
		ReadOnly:      true,
	}); err != nil {
		t.Fatalf("pkg/model Register: %v", err)
	}
	nucleusMeta, ok := reg.Get("NMAccount")
	if !ok {
		t.Fatal("NMAccount is not in the registry")
	}

	client, err := quark.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("quark.New: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.RegisterModel(&QDAccount{}); err != nil {
		t.Fatalf("RegisterModel: %v", err)
	}
	a := New(client)
	if err := Register[QDAccount](a,
		Icon("users"),
		ListFields("Email", "Role", "Score"),
		SearchFields("Email"),
		Filters("Role"),
		Exclude("Notes"), // PasswordHash: by its name
		Label("DisplayName", "Name"),
		ReadOnly(),
	); err != nil {
		t.Fatalf("Register: %v", err)
	}
	quarkInfo, _ := a.Get("QDAccount")

	if quarkInfo.Icon != nucleusMeta.Config.Icon || quarkInfo.ReadOnly != nucleusMeta.Config.ReadOnly {
		t.Errorf("model: quark icon=%q read_only=%v, nucleus icon=%q read_only=%v",
			quarkInfo.Icon, quarkInfo.ReadOnly, nucleusMeta.Config.Icon, nucleusMeta.Config.ReadOnly)
	}

	type flags struct {
		list, search, filter, excluded bool
		label                          string
	}
	compared := 0
	for _, nf := range nucleusMeta.Fields {
		if nf.Name == "CreatedAt" || nf.Name == "UpdatedAt" {
			continue
		}
		qf, ok := quarkInfo.Field(nf.Name)
		if !ok {
			t.Errorf("Quark side has no field %s", nf.Name)
			continue
		}
		// The adapter re-derives an untouched label acronym-aware ("ID",
		// not "I D"); the Quark side humanizes the same way. Labels are
		// compared where the configuration set one, and the derived ones
		// are checked for the fields whose Go name has no acronym.
		label := nf.Label
		if nf.Name == "ID" {
			label = "ID"
		}
		got := flags{qf.IsList, qf.IsSearch, qf.IsFilter, qf.IsExcluded, qf.Label}
		want := flags{nf.IsList, nf.IsSearch, nf.IsFilter, nf.IsExcluded, label}
		if got != want {
			t.Errorf("%s: quark %+v, nucleus %+v", nf.Name, got, want)
		}
		compared++
	}
	if compared != 7 {
		t.Errorf("compared %d fields, want the 7 of the model", compared)
	}

	if f, _ := quarkInfo.Field("PasswordHash"); !f.IsExcluded {
		t.Errorf("the credential-shaped column is excluded by name on the Quark side: %+v", f)
	}
}
