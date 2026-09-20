//go:build android

package main

// On a phone: the public API, always.
//
// A phone has no environment to configure and no localhost worth
// reaching, so the packaged app must work with no setup. This is a
// separate file rather than a runtime check so a desktop default
// cannot be compiled into the APK at all.
const defaultAPI = "https://pokemon.home.chrisscotmartin.com/api"
