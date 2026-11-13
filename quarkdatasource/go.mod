module github.com/jcsvwinston/orbit/quarkdatasource

go 1.26.6

toolchain go1.26.11

// quarkdatasource is an opt-in module that implements Orbit's datasource
// contract (orbit ADR-001) over a Quark ORM client, so Data Studio can browse
// and edit Quark-managed models (QADR-0006, Caso 2). It depends on BOTH Quark
// and the orbit root module (for the contract package) — which is exactly why
// it is a separate module: Quark must never enter the orbit core's dependency
// graph, and Quark itself must not depend on Orbit.
require (
	github.com/jcsvwinston/quark v1.19.0
	modernc.org/sqlite v1.58.0 // indirect
)

require (
	github.com/google/uuid v1.6.0
	github.com/jcsvwinston/nucleus v1.35.0
	github.com/jcsvwinston/quark/drivers/sqlite v0.3.2
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/redis/go-redis/v9 v9.22.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.46.0 // indirect
	go.opentelemetry.io/otel/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk v1.46.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.46.0 // indirect
	go.opentelemetry.io/otel/trace v1.46.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.15 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.30.4 // indirect
	github.com/jcsvwinston/orbit/datasource v1.1.2
	github.com/leodido/go-urn v1.5.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/crypto v0.56.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	modernc.org/libc v1.75.6 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
