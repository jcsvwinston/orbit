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
package fleettest
