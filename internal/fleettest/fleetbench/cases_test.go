// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// controls is the bench: every capability the fleet plane is measured on,
// with the verdict this repository RECORDS for it, one family per file.
//
// The list is a policy choice, not a derivation of what happens to exist.
// It comes from what an operator needs from a fleet product — who an agent
// is, whether the fleet's data screen obeys the application's own rules,
// what survives a restart, who gets told when a node goes quiet, what
// happens with more than one server, and whether the two web UIs are one
// product — which is the subject of arc A9.
//
// A verdict is what the probe MEASURED on the day it was recorded. Moving
// one is a deliberate edit in the change that moves the code.
func controls() []control {
	var all []control
	all = append(all, controlsIdentity()...)
	all = append(all, controlsDatasource()...)
	all = append(all, controlsRetention()...)
	all = append(all, controlsAlerts()...)
	all = append(all, controlsHA()...)
	all = append(all, controlsUI()...)
	return all
}
