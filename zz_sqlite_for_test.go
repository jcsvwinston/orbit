// Copyright 2026 jcsvwinston/orbit
// SPDX-License-Identifier: Apache-2.0

package orbit

// Nucleus links no database driver: each ships as its own module (nucleus
// ADR-031). Orbit does not open databases in production — it uses the *sql.DB
// the host application hands it, and the host links its own driver module —
// but these tests do, so the test binary links one the way an application
// would.
//
// It imports the nucleus MODULE rather than the driver package directly,
// which is what an application writes: the module registers the driver AND
// the error classifier. Without the classifier isBootstrapDuplicateError does
// not fail — it answers false, and a duplicate admin username surfaces as an
// internal error. The live test below caught exactly that.
import _ "github.com/jcsvwinston/nucleus/drivers/sqlite"
