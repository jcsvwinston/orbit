// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package fleetbench

// Nucleus links no database driver: each ships as its own module. The
// bench opens SQLite by default and honours ORBIT_TEST_DATASTUDIO_URL for
// a real engine, so the test binary links the three the way the fleet
// integration suite does — through the nucleus modules, which register the
// driver AND the error classifier.
import (
	_ "github.com/jcsvwinston/nucleus/drivers/mysql"
	_ "github.com/jcsvwinston/nucleus/drivers/postgres"
	_ "github.com/jcsvwinston/nucleus/drivers/sqlite"
)
