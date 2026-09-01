// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package server_test

// Nucleus links no database driver: each ships as its own module (nucleus
// ADR-031). The Data Studio suites open real PostgreSQL and MySQL in CI, and
// SQLite locally, so the test binary links the three the way an application
// would — through the nucleus modules, which register the driver AND the
// error classifier.
//
// The admin server itself opens no database in production: it serves what the
// agents report over gRPC. These requires exist for the tests, and a consumer
// of this module links none of them, since Go links only what is imported.
import (
	_ "github.com/jcsvwinston/nucleus/drivers/mysql"
	_ "github.com/jcsvwinston/nucleus/drivers/postgres"
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)
