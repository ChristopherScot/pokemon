//go:build !android

package main

// On a desktop: whatever `make run` started.
//
// A desktop build of this app is a development build - it exists to
// play the game while working on it - so pointing it at the cluster
// means local changes are invisible and a playthrough silently tests
// production. PORT_API in the root Makefile is 3000, and POKEDEX_URL
// overrides this for any other port.
const defaultAPI = "http://127.0.0.1:3000"
