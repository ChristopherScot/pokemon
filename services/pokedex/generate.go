package main

// `go generate ./...` regenerates api/ from the spec; CI runs it and fails
// on a diff, so a spec change cannot ship without matching code. Add every
// generator here as a //go:generate directive: a generator invoked from a
// Makefile step alone escapes CI's staleness gate.
//
// The ogen version is pinned: with @latest the build breaks on the day
// ogen publishes, in a file nobody touched.
//
//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.24.0 --config ogen.yml --target api --clean --package api openapi.yml
