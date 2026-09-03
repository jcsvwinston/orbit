package auth

import (
	"errors"
	"testing"
)

// A malformed trusted-proxy CIDR used to be skipped silently, so a typo in
// --ui-trusted-cidrs shrank the trusted set without anyone noticing.
// Validation now fails fast at boot.
func TestValidateTrustedCIDRs(t *testing.T) {
	if err := ValidateTrustedCIDRs(nil); err != nil {
		t.Fatalf("empty list (loopback defaults) must validate, got %v", err)
	}
	if err := ValidateTrustedCIDRs([]string{"10.0.0.0/8", " ::1/128 ", ""}); err != nil {
		t.Fatalf("well-formed entries must validate, got %v", err)
	}
	err := ValidateTrustedCIDRs([]string{"10.0.0.0/8", "10.42.0.0"})
	if err == nil {
		t.Fatal("an entry without a prefix length must be rejected")
	}
	if !errors.Is(err, ErrTrustedProxyMisconfigured) {
		t.Fatalf("error must wrap ErrTrustedProxyMisconfigured, got %v", err)
	}
}
