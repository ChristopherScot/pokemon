package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// A readiness probe only fails on a non-2xx status. If a failing
// readiness check returns 200 with an error body, kubelet sees a
// healthy pod and keeps sending it traffic - which is the exact bug
// /readyz exists to fix, reintroduced one layer down.
//
// So this asserts the STATUS CODE, not the body. The handler returns
// api.Error, and whether that renders as 503 is a property of the
// generated server, not of the handler.
func TestReadyzReturns503WhenTheDatabaseIsUnreachable(t *testing.T) {
	s := service{
		dex:     &pokedex{},
		battles: newMemStore(1),
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

// And a healthy one is 200, or the pod never joins its Service.
func TestReadyzReturns200WhenTheDatabaseAnswers(t *testing.T) {
	s := service{
		dex:     &pokedex{},
		battles: newMemStore(1),
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
