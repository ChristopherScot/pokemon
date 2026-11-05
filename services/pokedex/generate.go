package main

// Your generators go here, as //go:generate directives. `go generate
// ./...` runs them and CI fails on a diff, so a generator invoked from
// a Makefile step alone escapes that gate.
//
// This file is yours: `homelabctl regen` never rewrites it, so what
// you add here survives. The clients are generated from
// generate_clients.go, which regen does rewrite.
