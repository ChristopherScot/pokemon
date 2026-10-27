package battleclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A pokedex that accepts the connection and never answers. This client
// used ogen's constructor, whose http.Client has no timeout at all, so
// a stalled server held the caller until the user gave up.
func TestAStalledPokedexDoesNotHangTheCaller(t *testing.T) {
	// Released when the test is done, so the server's handler returns
	// and Close does not block for the length of a sleep.
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer stalled.Close()
	defer close(release)

	c, err := New(Identity{Name: "ash", Token: "t", API: stalled.URL}, "test")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	// A POST, so retryable() does not repeat it: one attempt, one timeout.
	_, err = c.Create(context.Background(), []string{"pikachu"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("a server that never answers returned no error")
	}
	// 5s bounds ONE attempt, so a retried call can legitimately take
	// longer; this bound only holds because Create is not retried.
	// Loose enough for a slow machine, tight enough that no timeout
	// at all runs until the harness kills it.
	if elapsed > 10*time.Second {
		t.Errorf("Create waited %v for a server that never answers: %v", elapsed, err)
	}
}
