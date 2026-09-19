package battleclient

import (
	"context"
	"errors"
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

func TestUnauthorizedIsAStaleIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"message":"unknown trainer token; register first"}`)
	}))
	defer srv.Close()

	c, err := New(Identity{Name: "t", Token: "dead", API: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"create", func() error { _, err := c.Create(context.Background(), nil); return err }},
		{"join", func() error { _, err := c.Join(context.Background(), "x", nil); return err }},
		{"attack", func() error { _, err := c.Attack(context.Background(), "x", 0, 0, 0); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, ErrStaleIdentity) {
				t.Errorf("got %v, which is not ErrStaleIdentity - callers cannot detect it", err)
			}
		})
	}
}

// A spectator is not a participant, and must not be told otherwise.
//
// SideFor used to check only Sides[0]: a name matching neither side
// fell through to "then it must be side 1" and returned ok=true with
// the sides swapped. The CLI's `watch <id>` and `battle <id>` accept
// any battle id, so pasting a link someone shared labelled a stranger's
// team "you" - and MyTurn was coincidentally false, so it printed
// "waiting on X" and looked plausible.
func TestSideForRefusesANonParticipant(t *testing.T) {
	b := &api.Battle{Sides: []api.Side{
		{Trainer: "ash"}, {Trainer: "misty"},
	}}

	c := &Client{Name: "brock"} // watching, in neither side
	if _, _, ok := c.SideFor(b); ok {
		t.Error("a spectator was reported as a participant")
	}
	if _, _, ok := c.SideIndex(b); ok {
		t.Error("SideIndex reported a spectator as a participant")
	}
}

// Both participants still resolve, in the right order.
func TestSideForOrientsEachParticipant(t *testing.T) {
	b := &api.Battle{Sides: []api.Side{
		{Trainer: "ash"}, {Trainer: "misty"},
	}}

	for _, tc := range []struct {
		name, mine, theirs string
		mi, ti             int
	}{
		{"ash", "ash", "misty", 0, 1},
		{"misty", "misty", "ash", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{Name: tc.name}
			mine, theirs, ok := c.SideFor(b)
			if !ok {
				t.Fatalf("%s is in this battle but SideFor said otherwise", tc.name)
			}
			if mine.Trainer != tc.mine || theirs.Trainer != tc.theirs {
				t.Errorf("SideFor = (%s, %s), want (%s, %s)", mine.Trainer, theirs.Trainer, tc.mine, tc.theirs)
			}
			mi, ti, ok := c.SideIndex(b)
			if !ok || mi != tc.mi || ti != tc.ti {
				t.Errorf("SideIndex = (%d, %d, %v), want (%d, %d, true)", mi, ti, ok, tc.mi, tc.ti)
			}
		})
	}
}

// mon builds a battle Pokemon with the given moves, none disabled.
func testMon(name string, fainted bool, moves ...string) api.BattlePokemon {
	p := api.BattlePokemon{Name: name, Hp: 10, MaxHp: 10, Fainted: fainted}
	if fainted {
		p.Hp = 0
	}
	for _, m := range moves {
		p.Moves = append(p.Moves, api.Move{Name: m})
	}
	return p
}

func activeBattleFor(mine, theirs []api.BattlePokemon, turn string) *api.Battle {
	return &api.Battle{
		ID:     "abc123",
		Status: api.BattleStatusActive,
		Turn:   api.NewOptString(turn),
		Sides: []api.Side{
			{Trainer: "ash", Team: mine},
			{Trainer: "misty", Team: theirs},
		},
	}
}

// A legal turn is legal.
func TestCheckTurnAllowsALegalTurn(t *testing.T) {
	b := activeBattleFor(
		[]api.BattlePokemon{testMon("pikachu", false, "thunderbolt")},
		[]api.BattlePokemon{testMon("staryu", false, "bubble")},
		"ash",
	)
	c := &Client{Name: "ash"}
	if err := c.CheckTurn(b, Turn{0, 0, 0}); err != nil {
		t.Errorf("a legal turn was refused: %v", err)
	}
}

// Each rule, in isolation.
func TestCheckTurnCatchesEachIllegalCase(t *testing.T) {
	alive := func() []api.BattlePokemon {
		return []api.BattlePokemon{testMon("pikachu", false, "thunderbolt", "quick-attack")}
	}
	foe := func() []api.BattlePokemon {
		return []api.BattlePokemon{testMon("staryu", false, "bubble")}
	}

	for _, tc := range []struct {
		name string
		mut  func(*api.Battle)
		turn Turn
		want error
	}{
		{"battle over", func(b *api.Battle) { b.Status = api.BattleStatusFinished }, Turn{0, 0, 0}, ErrBattleOver},
		{"not started", func(b *api.Battle) { b.Status = api.BattleStatusWaiting }, Turn{0, 0, 0}, ErrNotActive},
		{"other player's turn", func(b *api.Battle) { b.Turn = api.NewOptString("misty") }, Turn{0, 0, 0}, ErrNotYourTurn},
		{"no such attacker", nil, Turn{3, 0, 0}, ErrNoSuchMon},
		{"no such target", nil, Turn{0, 0, 3}, ErrNoSuchMon},
		{"attacker has fainted", func(b *api.Battle) { b.Sides[0].Team[0] = testMon("pikachu", true, "thunderbolt") }, Turn{0, 0, 0}, ErrFainted},
		{"no such move", nil, Turn{0, 9, 0}, ErrNoSuchMove},
		{"target already down", func(b *api.Battle) { b.Sides[1].Team[0] = testMon("staryu", true, "bubble") }, Turn{0, 0, 0}, ErrTargetDown},
		{"move disabled", func(b *api.Battle) { b.Sides[0].Team[0].DisabledMove = api.NewOptInt(1) }, Turn{0, 1, 0}, ErrDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := activeBattleFor(alive(), foe(), "ash")
			if tc.mut != nil {
				tc.mut(b)
			}
			c := &Client{Name: "ash"}
			err := c.CheckTurn(b, tc.turn)
			if !errors.Is(err, tc.want) {
				t.Errorf("CheckTurn = %v, want %v", err, tc.want)
			}
		})
	}
}

// A spectator gets the same answer the server gives them.
func TestCheckTurnRefusesASpectator(t *testing.T) {
	b := activeBattleFor(
		[]api.BattlePokemon{testMon("pikachu", false, "thunderbolt")},
		[]api.BattlePokemon{testMon("staryu", false, "bubble")},
		"ash",
	)
	c := &Client{Name: "brock"}
	if err := c.CheckTurn(b, Turn{0, 0, 0}); !errors.Is(err, ErrNotYourTurn) {
		t.Errorf("a spectator got %v, want ErrNotYourTurn", err)
	}
}

// The ORDER is the point, not just the set.
//
// A client that reports a different FIRST reason than the server would
// teach the player a rule that is not the rule. With several things
// wrong at once, the earliest server check must win: here the battle is
// over AND it is not this player's turn AND the attacker has fainted -
// the answer is "this battle is over".
func TestCheckTurnReportsTheSameFirstReasonAsTheServer(t *testing.T) {
	c := &Client{Name: "ash"}

	over := activeBattleFor(
		[]api.BattlePokemon{testMon("pikachu", true, "thunderbolt")},
		[]api.BattlePokemon{testMon("staryu", true, "bubble")},
		"misty",
	)
	over.Status = api.BattleStatusFinished
	if err := c.CheckTurn(over, Turn{9, 9, 9}); !errors.Is(err, ErrBattleOver) {
		t.Errorf("everything wrong at once = %v, want ErrBattleOver first", err)
	}

	// Not your turn outranks a bad index, as it does on the server.
	notYours := activeBattleFor(
		[]api.BattlePokemon{testMon("pikachu", false, "thunderbolt")},
		[]api.BattlePokemon{testMon("staryu", false, "bubble")},
		"misty",
	)
	if err := c.CheckTurn(notYours, Turn{9, 9, 9}); !errors.Is(err, ErrNotYourTurn) {
		t.Errorf("bad indices on someone else's turn = %v, want ErrNotYourTurn", err)
	}

	// A fainted ATTACKER outranks a bad move index, as it does on the
	// server - attacker.fainted() is checked before the move range.
	downed := activeBattleFor(
		[]api.BattlePokemon{testMon("pikachu", true, "thunderbolt")},
		[]api.BattlePokemon{testMon("staryu", false, "bubble")},
		"ash",
	)
	if err := c.CheckTurn(downed, Turn{0, 9, 0}); !errors.Is(err, ErrFainted) {
		t.Errorf("fainted attacker with a bad move = %v, want ErrFainted first", err)
	}
}
