package main

import (
	"strings"
	"testing"
)

// The default is chosen at compile time, so what this asserts depends
// on which build you are running. Both directions matter: a phone that
// defaults to localhost reaches nothing, and a desktop that defaults to
// the cluster makes every local playthrough test production instead of
// the code being changed.
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
		t.Errorf("desktop default is %q, want a local server: a desktop build is a "+
			"development build, and pointing it at the cluster means a playthrough "+
			"tests production rather than the change being made", got)
	}
}

// Whitespace is not a URL. Exported as "" or " " it would otherwise
// point the app at nothing, failing with a parse error rather than
// falling back.
func TestBlankOverrideFallsBackRatherThanBreaking(t *testing.T) {
	for _, v := range []string{"", " ", "\t", "\n"} {
		t.Setenv("POKEDEX_URL", v)
		if got := apiBase(); got != defaultAPI {
			t.Errorf("apiBase() = %q for a blank override %q, want the default %q", got, v, defaultAPI)
		}
	}
}

// The override is what lets a playthrough point at a server on another
// port, and it has to win on both platforms.
func TestOverrideWins(t *testing.T) {
	t.Setenv("POKEDEX_URL", "http://127.0.0.1:19000")
	if got, want := apiBase(), "http://127.0.0.1:19000"; got != want {
		t.Errorf("apiBase() = %q, want %q", got, want)
	}
}
