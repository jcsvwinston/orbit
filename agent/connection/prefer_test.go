package connection

import "testing"

func TestDialer_PreferOnlyConfiguredEndpoints(t *testing.T) {
	d := NewDialer(Config{Endpoints: []string{"http://a:9090", "http://b:9090"}})
	if d.Prefer("http://c:9090") {
		t.Fatal("an endpoint the operator did not configure is refused")
	}
	if got := d.ordered(); got[0] != "http://a:9090" {
		t.Fatalf("without a preference the configured order stands: %v", got)
	}
	if !d.Prefer(" http://b:9090 ") {
		t.Fatal("a configured endpoint is accepted, trimmed")
	}
	if got := d.ordered(); len(got) != 2 || got[0] != "http://b:9090" || got[1] != "http://a:9090" {
		t.Fatalf("the preferred endpoint goes first, the rest keep their order: %v", got)
	}
	if d.Preferred() != "http://b:9090" {
		t.Fatal("Preferred reports the choice")
	}
}
