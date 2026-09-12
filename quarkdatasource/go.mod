module github.com/jcsvwinston/orbit/quarkdatasource

go 1.26.6

// quarkdatasource is an opt-in module that implements Orbit's datasource
// contract (orbit ADR-001) over a Quark ORM client, so Data Studio can browse
// and edit Quark-managed models (QADR-0006, Caso 2). It depends on BOTH Quark
// and the orbit root module (for the contract package) — which is exactly why
// it is a separate module: Quark must never enter the orbit core's dependency
// graph, and Quark itself must not depend on Orbit.
require (
	github.com/jcsvwinston/orbit v1.9.1
	github.com/jcsvwinston/quark v1.14.0
	modernc.org/sqlite v1.58.0 // indirect
)

require (
	github.com/google/uuid v1.6.0
	github.com/jcsvwinston/nucleus v1.28.0
	github.com/jcsvwinston/quark/drivers/sqlite v0.2.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.4 // indirect
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
