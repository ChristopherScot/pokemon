package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// Kubelet only fails a readiness probe on a non-2xx, so this asserts
// the status code, not the body: how api.Error renders is a property
// of the generated server.
func TestReadyzReturns503WhenTheDatabaseIsUnreachable(t *testing.T) {
	s := service{
		dex:     &pokedex{},
		battles: newMemStore(),
		rng:     rngFor(1),
		ready: func(context.Context) error {
			return errors.New("dial tcp: connection refused")
		},
	}
	srv, err := api.NewServer(s)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz with a dead database returned %d, want 503 - a "+
			"readiness probe only fails on a non-2xx, so anything else means "+
			"kubelet keeps routing traffic to a pod that cannot serve", rec.Code)
	}
}

func TestReadyzReturns200WhenTheDatabaseAnswers(t *testing.T) {
	s := service{
		dex:     &pokedex{},
		battles: newMemStore(),
		rng:     rngFor(1),
		ready:   func(context.Context) error { return nil },
	}
	srv, err := api.NewServer(s)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("GET /readyz with a healthy database returned %d, want 200", rec.Code)
	}
}
