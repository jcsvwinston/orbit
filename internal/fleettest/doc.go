// Copyright 2026 jcsvwinston
// SPDX-License-Identifier: Apache-2.0

// Package fleettest holds the integration tests that drive a real
// agent.Agent against a real server.Server. It is its own Go module so that
// the server module does not have to require the agent module by tag — the
// requirement that used to force a convergence cut on every release (ADR-006).
//
// The module is not published: the `internal` path element makes it
// unimportable outside github.com/jcsvwinston/orbit, it has no entry in
// release-please, and it is only ever built through the repository's go.work.
//
// The fleetbench/ package inside this module is the fleet plane's measured
// inventory: fifty controls — who an agent is, whether the fleet's data
// screen obeys the application's rules, what survives a restart, who gets
// told, more than one server, one web UI or two — each with a probe that
// boots real servers and agents (or reads the protocol descriptors and the
// checked-out tree) and a RECORDED verdict the probe is checked against.
// It runs with the rest of this module's tests; docs/fleet-bench.md is the
// page its table is published on, and ORBIT_FLEET_BENCH_TABLE=1 regenerates
// that table from the catalogue.
package fleettest
