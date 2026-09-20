package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := handler()
	if err != nil {
		t.Fatalf("handler() = %v", err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestHealthz(t *testing.T) {
	w := get(t, newHandler(t), "/healthz")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"ok"`) {
		t.Errorf("body = %q, want it to report ok", w.Body.String())
	}
}

func TestRootIdentifiesTheService(t *testing.T) {
	w := get(t, newHandler(t), "/")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"pokedex"`) {
		t.Errorf("body = %q, want it to name the service", w.Body.String())
	}
}

// A path that is not in the spec is rejected by the generated router.
func TestUnknownPathIsNotFound(t *testing.T) {
	if w := get(t, newHandler(t), "/nope"); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestMetricsAreExposed(t *testing.T) {
	h := newHandler(t)
	get(t, h, "/") // a request to record

	w := get(t, h, "/metrics")
	if w.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	// Labelled by route and by the REAL status. It used to be keyed
	// on the operation id with a status inferred from whether the
	// handler returned a Go error, which made every typed 401 and
	// 409 look like a 200.
	if !strings.Contains(body, `http_requests_total{method="GET",route="/",status="200"`) {
		t.Errorf("no request counter for the root route:\n%s", body)
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Error("no runtime metrics in /metrics")
	}
}

func TestRequestPathNeverBecomesALabel(t *testing.T) {
	h := newHandler(t)
	get(t, h, "/secret-token-value/deadbeef")

	if body := get(t, h, "/metrics").Body.String(); strings.Contains(body, "deadbeef") {
		t.Error("a request path reached a metric label")
	}
}
