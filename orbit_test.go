package orbit

import "testing"

// Module is a nucleus ModuleSpec with orbit's identity and the default prefix.
func TestModule_Identity(t *testing.T) {
	spec := Module(Config{})
	if spec.Name() != "orbit" {
		t.Errorf("Name = %q, want orbit", spec.Name())
	}
	if spec.Prefix() != DefaultPrefix {
		t.Errorf("default Prefix = %q, want %q", spec.Prefix(), DefaultPrefix)
	}
}

// A custom prefix is honoured.
func TestModule_CustomPrefix(t *testing.T) {
	if got := Module(Config{Prefix: "/backoffice"}).Prefix(); got != "/backoffice" {
		t.Errorf("Prefix = %q, want /backoffice", got)
	}
}
