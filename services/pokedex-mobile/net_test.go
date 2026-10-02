package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The spec's own ceiling; asking for more is a 400.
const specMaxLimit = 100

func TestLoadDexAsksForALimitTheAPIAccepts(t *testing.T) {
	var gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		// Reject as the real server does, so a bad limit fails loudly.
		if n, err := strconv.Atoi(gotLimit); err == nil && n > specMaxLimit {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error_message": "value " + gotLimit + " greater than " + strconv.Itoa(specMaxLimit),
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"pokemon": []map[string]any{{
				"id": 25, "name": "pikachu", "types": []string{"electric"},
				"height": 4, "weight": 60, "sprite": "", "moves": []string{},
				"description": "", "genus": "",
			}},
		})
	}))
	defer srv.Close()

	a := newUI(nil)
	defer close(a.done)
	c, err := api.NewClient(srv.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	a.api = c

	a.loadDex()
	r := waitResult(t, a)

	if n, convErr := strconv.Atoi(gotLimit); convErr == nil && n > specMaxLimit {
		t.Errorf("loadDex asked for limit=%s; the spec caps it at %d, so every request 400s",
			gotLimit, specMaxLimit)
	}
	if r.err != nil {
		t.Fatalf("loadDex failed: %v", r.err)
	}
	if len(r.dex) == 0 {
		t.Error("loadDex returned nothing; the Pokedex would be empty")
	}
}

// A 400 must reach the player as something readable, not as ogen's decode failure.
func TestServerErrorsAreReadable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ogen validation answers with error_message where the spec declares message, so the generated client cannot decode it.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error_message": "operation RegisterTrainer: decode request: validate: invalid",
		})
	}))
	defer srv.Close()

	a := newUI(nil)
	defer close(a.done)
	c, _ := api.NewClient(srv.URL)
	a.api = c
	a.loadDex()
	r := waitResult(t, a)

	if r.err == nil {
		t.Fatal("a 400 was not reported as an error")
	}
	msg := statusFor(r.err)
	for _, leak := range []string{"decode response", "field required", "application/json"} {
		if contains(msg, leak) {
			t.Errorf("the player would see %q, which contains the raw %q", msg, leak)
		}
	}
	if msg == "" {
		t.Error("an error produced no message at all")
	}
}

func waitResult(t *testing.T, a *ui) result {
	t.Helper()
	select {
	case r := <-a.results:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("no result within 10s")
		return result{}
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) &&
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()
}
