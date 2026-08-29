package orbit

import (
	"reflect"
	"strings"
	"testing"
)

// TestConfig_EveryFieldCarriesAKoanfTag pins the tag orbit.Config must be
// bindable by.
//
// Nucleus binds a module's `modules.<name>.*` subtree with
// `raw.Unmarshal("", &cfg)` (pkg/nucleus/module.go), and koanf's default
// struct tag is `koanf` — nucleus never sets Tag: "yaml" anywhere. Config
// carried only `yaml:` tags, so 16 of its 19 keys were dropped in silence:
//
//	claves presentes en el sub-koanf de modules.orbit: 19
//	  OK    Prefix              = "/panel"
//	  OK    Title               = "Quantum Coverage Ops"
//	  CERO  BootstrapUsername   = ""
//	  CERO  BootstrapPassword   = ""
//	  CERO  AuthDatabase        = ""
//	  … (16 en total)
//
// Exactly the single-word keys survived (prefix, title, environment),
// because mapstructure falls back to comparing the FIELD NAME
// case-insensitively when it finds no tag. Nothing in snake_case has any
// way to map, which is why `bootstrap_password` — the key this defect was
// first opened for — never arrived.
//
// The guard is structural rather than a single binding assertion on
// purpose: it fails for every field added later without the tag, which is
// how the defect got in.
func TestConfig_EveryFieldCarriesAKoanfTag(t *testing.T) {
	rt := reflect.TypeOf(Config{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
			continue // unexported
		}
		yamlTag := strings.Split(f.Tag.Get("yaml"), ",")[0]
		koanfTag := strings.Split(f.Tag.Get("koanf"), ",")[0]

		if yamlTag == "" {
			t.Errorf("%s: no yaml tag; the field is undocumented as a config key", f.Name)
			continue
		}
		if koanfTag == "" {
			t.Errorf("%s: has yaml:%q but no koanf tag — nucleus binds with the koanf tag, "+
				"so this key is silently dropped from modules.orbit.*", f.Name, yamlTag)
			continue
		}
		// The two must agree, or the documented key and the bound key differ.
		if koanfTag != yamlTag {
			t.Errorf("%s: yaml:%q != koanf:%q — the documented key and the bound key must be the same",
				f.Name, yamlTag, koanfTag)
		}
	}
}
