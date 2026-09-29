package main

import (
	"strings"
	"testing"
)

// Default is chosen at compile time: phone must reach the public API, desktop must default to a local server.
func TestTheDefaultSuitsThePlatform(t *testing.T) {
	t.Setenv("POKEDEX_URL", "")
	got := apiBase()
	if isAndroid {
		if !strings.HasPrefix(got, "https://") {
			t.Errorf("android default is %q, want the public https API", got)
		}
		if strings.Contains(got, "127.0.0.1") || strings.Contains(got, "localhost") {
			t.Errorf("android default is %q - a phone cannot route to localhost", got)
		}
		return
	}
	if !strings.Contains(got, "127.0.0.1") {
		t.Errorf("desktop default is %q, want a local server", got)
	}
}

// Whitespace overrides must fall back to the default rather than pointing the app at nothing.
func TestBlankOverrideFallsBackRatherThanBreaking(t *testing.T) {
	for _, v := range []string{"", " ", "\t", "\n"} {
		t.Setenv("POKEDEX_URL", v)
		if got := apiBase(); got != defaultAPI {
			t.Errorf("apiBase() = %q for a blank override %q, want the default %q", got, v, defaultAPI)
		}
	}
}

func TestOverrideWins(t *testing.T) {
	t.Setenv("POKEDEX_URL", "http://127.0.0.1:19000")
	if got, want := apiBase(), "http://127.0.0.1:19000"; got != want {
		t.Errorf("apiBase() = %q, want %q", got, want)
	}
}
