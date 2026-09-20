package main

// Everything derived from a file in this directory is generated here, so
// `go generate ./...` - and therefore `homelabctl regen`, which runs it -
// regenerates all of it. CI runs regen and fails on a diff, so a
// generator missing from this file is a generator CI cannot check.
//
// Both are pinned to an exact version. CI's check is "regenerate, then
// diff", which only means something if the same input gives the same
// output; @latest makes the build fail on the day a generator publishes,
// in a file nobody edited, reported as committed code being stale.

//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.24.0 --config ogen.yml --target api --clean --package api openapi.yml

// sqlc, whose directive was missing entirely: sqlc.yaml said this line
// was "in db.go, alongside the ogen one" and it was in neither file - so
// editing queries/ or migrations/ and running `go generate ./...`
// regenerated the API and silently left internal/dbgen untouched. The
// first sign of a new query was `undefined:` at the call site, and the
// staleness gate passed on a database layer that no longer matched its
// SQL.
//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
