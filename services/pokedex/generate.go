package main

// Every generator is pinned to an exact version so CI's regen-then-diff
// gate is reproducible.

//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.24.0 --config ogen.yml --target api --clean --package api openapi.yml
//go:generate go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
