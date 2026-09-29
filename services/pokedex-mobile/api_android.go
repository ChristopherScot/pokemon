//go:build android

package main

// Separate file, not a runtime switch, so a desktop default cannot be compiled into the APK.
const defaultAPI = "https://pokemon.home.chrisscotmartin.com/api"
