// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// QCD-OR-1: the modules.orbit.* binding the README documents was inert.
//
// README.md states that "orbit.Config is bound from the modules.orbit.*
// subtree of your nucleus.yml" and lists the keys — prefix, title,
// environment, bootstrap_username, bootstrap_email and bootstrap_password.
// The framework did its half: bindModuleConfigs extracts the subtree and
// hands the bound value to the hooks as the C parameter. Orbit threw it
// away and closed over the construction-time config instead.
//
// Because the module IS mounted, warnUnmountedModuleConfigs stayed quiet
// too: the configuration was ignored in complete silence. Someone who
// believed they had set bootstrap_password had not set it.
//
// First noted five orbit releases ago as a minor remark; unchanged since,
// which is why it became a formal finding.
package orbit

import (
	"testing"
)

func TestModuleConfig_BoundValueWins(t *testing.T) {
	// Construction-time config: what the host wrote in Go.
	m := newModule(Config{
		Prefix:            DefaultPrefix,
		Title:             "from code",
		Environment:       "code-env",
		BootstrapPassword: "from-code",
	})

	// What the YAML subtree binds on top. The framework overlays it onto
	// the declared Config, key by key, so YAML wins per key and code
	// supplies the base — that is the precedence this test pins.
	bound := Config{
		Prefix:            DefaultPrefix,
		Title:             "from yaml",
		Environment:       "yaml-env",
		BootstrapPassword: "from-yaml",
	}

	effective := m.effectiveConfig(bound)

	if effective.Title != "from yaml" {
		t.Errorf("title: YAML must win, got %q", effective.Title)
	}
	if effective.Environment != "yaml-env" {
		t.Errorf("environment: YAML must win, got %q", effective.Environment)
	}
	if effective.BootstrapPassword != "from-yaml" {
		t.Errorf("bootstrap_password: YAML must win — this is the key with security weight, got %q", effective.BootstrapPassword)
	}
}

// The mount point is fixed when the module is built: the framework reads
// Module.Prefix before it binds the YAML subtree. Honouring a different
// prefix inside the hooks would generate links that do not match where the
// panel is actually mounted — worse than ignoring it. So it is refused
// loudly instead.
func TestModuleConfig_PrefixMismatchIsRefused(t *testing.T) {
	m := newModule(Config{Prefix: "/admin"})

	err := m.checkPrefixAgreement(Config{Prefix: "/panel"})
	if err == nil {
		t.Fatal("a YAML prefix that differs from the mount point must fail loudly, not produce broken links")
	}

	if err := m.checkPrefixAgreement(Config{Prefix: "/admin"}); err != nil {
		t.Errorf("the same prefix in both places is the normal case: %v", err)
	}
	if err := m.checkPrefixAgreement(Config{}); err != nil {
		t.Errorf("an unset YAML prefix must simply keep the mount point: %v", err)
	}
}
