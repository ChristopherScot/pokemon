//go:build !android

package main

// Desktop builds are development builds; they must reach `make run`'s local server (root Makefile's PORT_API 3000), not the cluster.
const defaultAPI = "http://127.0.0.1:3000"
