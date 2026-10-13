module github.com/jcsvwinston/orbit/quarkdatasource

go 1.26.6

toolchain go1.26.9

// quarkdatasource is an opt-in module that implements Orbit's datasource
// contract (orbit ADR-001) over a Quark ORM client, so Data Studio can browse
// and edit Quark-managed models (QADR-0006, Caso 2). It depends on BOTH Quark
// and the orbit root module (for the contract package) — which is exactly why
// it is a separate module: Quark must never enter the orbit core's dependency
// graph, and Quark itself must not depend on Orbit.
require (
	github.com/jcsvwinston/quark v1.17.0
	modernc.org/sqlite v1.60.1 // indirect
)

require (
	github.com/google/uuid v1.6.0
	github.com/jcsvwinston/nucleus v1.32.0
	github.com/jcsvwinston/quark/drivers/sqlite v0.3.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.4 // indirect
	github.com/jcsvwinston/orbit/datasource v1.0.1
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
