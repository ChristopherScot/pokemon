package main

//go:generate go run github.com/ogen-go/ogen/cmd/ogen@latest --config ogen.yml --target api --clean --package api openapi.yml

// sqlc, pinned: `go generate ./...` has to produce the same bytes for
// everyone, and CI fails the build when generated code is stale. @latest
// would make that check depend on the day you ran it.
//
// This line is what sqlc.yaml's comment promised and nothing provided -
// editing queries/ or migrations/ and running `go generate ./...`
// regenerated the API and silently left internal/dbgen untouched, so
// the first sign of a new query was `undefined:` at the call site.
//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
