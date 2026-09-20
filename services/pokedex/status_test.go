package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The status label used to be inferred as "200, or 500 if err != nil",
// so every typed 401/404/409 was counted as a success.
func TestStatusLabelIsTheRealStatus(t *testing.T) {
	for _, code := range []int{200, 401, 404, 409, 500} {
		rec := httptest.NewRecorder()
		h := countStatus(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/battles", nil))
		if rec.Code != code {
			t.Errorf("wrote %d, recorder saw %d", code, rec.Code)
		}
	}
}

// Battle ids in the path would give one metric series per battle.
func TestRouteLabelHasBoundedCardinality(t *testing.T) {
	for path, want := range map[string]string{
		"/api/battles/abc123":      "/api/battles/{id}",
		"/api/battles/xyz789/turn": "/api/battles/{id}/turn",
		"/api/battles":             "/api/battles",
		"/api/trainers/waiting":    "/api/trainers/waiting",
	} {
		got := routeOf(httptest.NewRequest(http.MethodGet, path, nil))
		if got != want {
			t.Errorf("routeOf(%q) = %q, want %q", path, got, want)
		}
	}
}
