package main

// `go generate ./...` regenerates api/ and clients/ts/ from the spec; CI
// runs it and fails on a diff, so a spec change cannot ship without
// matching code. Add every generator here as a //go:generate directive: a
// generator invoked from a Makefile step alone escapes CI's staleness gate.
//
// oapi-clientgen rather than a pinned `go run`: it runs ogen and hey-api
// itself, at the versions it pins, so this line does not name a version
// that would then need changing in every service. Install it with
// `go install github.com/ChristopherScot/oapi-clientgen@latest`.
//
//go:generate oapi-clientgen generate self ./openapi.yml --out api --ts-out clients/ts --npm-package @christopherscot/pokedex-client
