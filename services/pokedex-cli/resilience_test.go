package main

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The point of the migration: the client this binary builds retries a
// 503, where ogen's own constructor would hand it straight back.
func TestClientIsResilient(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":25,"name":"pikachu","description":"d","genus":"g","height":4,"weight":60,"sprite":"s","types":[],"stats":{"hp":1,"attack":1,"defense":1,"speed":1},"moves":[]}`))
	}))
	defer srv.Close()
	t.Setenv("POKEDEX_URL", srv.URL)

	c, err := client()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := withTimeout()
	defer cancel()
	if _, err := c.GetPokemon(ctx, api.GetPokemonParams{Name: "pikachu"}); err != nil {
		t.Fatalf("the 503 should have been retried: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}
