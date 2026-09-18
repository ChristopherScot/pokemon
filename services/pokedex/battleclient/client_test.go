package battleclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func battle(status api.BattleStatus, turn string, sides ...string) *api.Battle {
	b := &api.Battle{Status: status}
	if turn != "" {
		b.Turn = api.NewOptString(turn)
	}
	for _, s := range sides {
		b.Sides = append(b.Sides, api.Side{Trainer: s})
	}
	return b
}

// MyTurn exists because every caller needs it and comparing the wrong
// field is an easy mistake: a client that thinks it is always its turn
// spams 409s, and one that never thinks so hangs forever.
func TestMyTurn(t *testing.T) {
	c := &Client{Name: "ash"}
	for _, tc := range []struct {
		name string
		b    *api.Battle
		want bool
	}{
		{"my move", battle(api.BattleStatusActive, "ash", "ash", "gary"), true},
		{"their move", battle(api.BattleStatusActive, "gary", "ash", "gary"), false},
		// Waiting and finished have no turn; treating an absent turn as
		// "mine" would make a client attack into a battle that is over.
		{"still waiting", battle(api.BattleStatusWaiting, "", "ash"), false},
		{"finished", battle(api.BattleStatusFinished, "", "ash", "gary"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.MyTurn(tc.b); got != tc.want {
				t.Errorf("MyTurn() = %v, want %v", got, tc.want)
			}
		})
	}
}

// SideFor must not depend on which slot the server happened to put this
// trainer in - the creator is index 0 and the joiner is index 1, so a
// client that assumed one would be wrong half the time.
func TestSideForWorksFromEitherSlot(t *testing.T) {
	c := &Client{Name: "ash"}

	mine, theirs, ok := c.SideFor(battle(api.BattleStatusActive, "ash", "ash", "gary"))
	if !ok || mine.Trainer != "ash" || theirs.Trainer != "gary" {
		t.Errorf("as creator: mine=%q theirs=%q ok=%v", mine.Trainer, theirs.Trainer, ok)
	}

	mine, theirs, ok = c.SideFor(battle(api.BattleStatusActive, "ash", "gary", "ash"))
	if !ok || mine.Trainer != "ash" || theirs.Trainer != "gary" {
		t.Errorf("as joiner: mine=%q theirs=%q ok=%v", mine.Trainer, theirs.Trainer, ok)
	}

	// A battle with one side is not yet playable; ok must say so rather
	// than the caller indexing past the end.
	if _, _, ok := c.SideFor(battle(api.BattleStatusWaiting, "", "ash")); ok {
		t.Error("SideFor reported ok for a battle with one side")
	}
}

// An empty team means "pick for me", and has to be sent as an ABSENT
// field rather than an empty array.
//
// The spec says minItems 3, so `"team": []` is a validation error - and
// the 400 it produces does not even decode as the client's Error type,
// so the caller sees "invalid: message (field required)" instead of
// anything about teams. A nil slice is what makes ogen omit the field.
func TestEmptyTeamIsSentAsAbsent(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x","status":"waiting","version":1,"sides":[],"log":[]}`))
	}))
	defer srv.Close()

	c, err := New(Identity{Name: "ash", Token: "t", API: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, `"team":[]`) {
		t.Errorf("sent an empty array, which fails minItems: %s", got)
	}
	if strings.Contains(got, `"team"`) {
		t.Errorf("team should be absent entirely: %s", got)
	}
}

// Every Go client points at the same deployed API by default.
//
// They did not: the CLI defaulted to the in-cluster address while the
// TUI defaulted to the public URL. The CLI is published as a release
// binary and run on laptops, where pokedex.pokedex.svc.cluster.local
// does not resolve - and "no such host" reads as a broken install
// rather than a default that only works inside the cluster.
func TestDefaultAPIIsReachableFromAnywhere(t *testing.T) {
	if !strings.HasPrefix(DefaultAPI, "https://") {
		t.Errorf("DefaultAPI = %q, which is not a public address", DefaultAPI)
	}
	// An in-cluster name resolves nowhere else, so it cannot be the
	// default for a binary people download.
	if strings.Contains(DefaultAPI, ".svc.cluster.local") {
		t.Errorf("DefaultAPI = %q, an in-cluster address", DefaultAPI)
	}
}
