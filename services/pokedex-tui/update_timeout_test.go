package main

import (
	"net/http"
	"testing"
	"time"
)

// Both requests went through http.Get, which uses http.DefaultClient
// and has no timeout, so a mirror that accepted the connection and
// never answered left `pokedex-tui update` waiting until the user gave
// up. Asserted on the clients rather than by stalling a server: the
// download's bound is minutes, and a test that waits that long to
// prove a timeout exists would never be run.
func TestTheUpdateClientsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name   string
		client *http.Client
		most   time.Duration
	}{
		// Asking which release is newest is a small JSON call.
		{"apiClient", apiClient, 30 * time.Second},
		// A binary over a slow line is minutes, but not unbounded.
		{"downloadClient", downloadClient, 15 * time.Minute},
	} {
		switch {
		case tc.client.Timeout == 0:
			t.Errorf("%s has no timeout; a stalled mirror holds it forever", tc.name)
		case tc.client.Timeout > tc.most:
			t.Errorf("%s.Timeout = %v, want <= %v", tc.name, tc.client.Timeout, tc.most)
		}
	}
}
