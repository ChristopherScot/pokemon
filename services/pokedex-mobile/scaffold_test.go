package main

// Tests for the app's logic. Nothing here opens a window: Gio's layout
// needs a frame and CI has no display, so anything drawing is an
// integration test with a device in it. What IS testable is everything
// in state.go, which is why the decisions live there.

import (
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func mon(name string, hp, maxHP int, fainted bool) api.BattlePokemon {
	return api.BattlePokemon{
		Name: name, Hp: hp, MaxHp: maxHP, Fainted: fainted,
		Types: []string{"normal"},
		Moves: []api.Move{
			{Name: "tackle", Type: "normal", Power: 40, Pp: 35, DamageClass: "physical"},
			{Name: "growl", Type: "normal", Power: 0, Pp: 40, DamageClass: "status"},
		},
	}
}

// The version is stamped by CI with -ldflags. A released build
// reporting "dev" means that flag was dropped, which is invisible
// until someone asks a user what version they are running.
func TestVersionHasADefault(t *testing.T) {
	if version == "" {
		t.Error("version is empty; the ldflags default was removed")
	}
}

// Pick order is what the server reads as the team list, so the first
// pick leads. Removing from the middle must not resort the rest.
func TestToggleTeamKeepsPickOrder(t *testing.T) {
	var team []string
	for _, n := range []string{"pikachu", "geodude", "staryu"} {
		team = toggleTeam(team, n)
	}
	if got := strings.Join(team, ","); got != "pikachu,geodude,staryu" {
		t.Fatalf("team = %q, want pick order preserved", got)
	}

	team = toggleTeam(team, "geodude") // remove the middle
	if got := strings.Join(team, ","); got != "pikachu,staryu" {
		t.Errorf("after removing the middle, team = %q", got)
	}

	// Re-adding appends rather than restoring the old slot.
	team = toggleTeam(team, "geodude")
	if got := strings.Join(team, ","); got != "pikachu,staryu,geodude" {
		t.Errorf("re-adding should append, got %q", got)
	}
}

func TestToggleTeamStopsAtThree(t *testing.T) {
	team := []string{"a", "b", "c"}
	if got := toggleTeam(team, "d"); len(got) != 3 {
		t.Errorf("a fourth pick was accepted: %v", got)
	}
	// But deselecting still works when full, or the UI would be stuck.
	if got := toggleTeam(team, "b"); len(got) != 2 {
		t.Errorf("could not deselect from a full team: %v", got)
	}
}

func TestTeamPositionIsOneBased(t *testing.T) {
	team := []string{"pikachu", "geodude"}
	if got := teamPosition(team, "pikachu"); got != 1 {
		t.Errorf("lead position = %d, want 1", got)
	}
	if got := teamPosition(team, "staryu"); got != 0 {
		t.Errorf("unpicked position = %d, want 0", got)
	}
}

// A server reporting hp above maxHp - a heal, or a bug - must not draw
// a bar past the end of its track.
func TestHPFractionIsClamped(t *testing.T) {
	for _, tc := range []struct {
		hp, max int
		want    float32
	}{
		{20, 20, 1},
		{10, 20, 0.5},
		{0, 20, 0},
		{-5, 20, 0}, // already fainted, reported negative
		{30, 20, 1}, // healed past full
		{10, 0, 0},  // no max: avoid dividing by zero
	} {
		if got := hpFraction(mon("x", tc.hp, tc.max, false)); got != tc.want {
			t.Errorf("hpFraction(%d/%d) = %v, want %v", tc.hp, tc.max, got, tc.want)
		}
	}
}

// Status moves have power 0, which reads as a bug rather than a
// deliberate absence - the other clients show a dash and so must this.
func TestMoveLabelShowsADashForStatusMoves(t *testing.T) {
	if got := moveLabel(api.Move{Name: "growl", Power: 0}); !strings.Contains(got, "—") {
		t.Errorf("status move label = %q, want a dash", got)
	}
	if got := moveLabel(api.Move{Name: "tackle", Power: 40}); !strings.Contains(got, "40") {
		t.Errorf("damaging move label = %q, want the power", got)
	}
}

// The wording comes from battletext so three clients narrate a battle
// identically. Asserting the exact string here would duplicate that
// package's tests and drift from them; asserting that it REACHES the
// UI is the part this app can get wrong.
func TestSummariseUsesTheSharedWording(t *testing.T) {
	p := mon("pikachu", 20, 20, false)
	plain := summarise(p)
	if !strings.HasPrefix(plain, "pikachu") {
		t.Errorf("summarise() = %q, want it to start with the name", plain)
	}
	p.Confused = api.NewOptBool(true)
	if summarise(p) == plain {
		t.Error("a confused Pokemon rendered identically to a healthy one; battletext.Conditions is not reaching the UI")
	}
}

// The server addresses Pokemon by position, so a fainted one must not
// renumber the others.
func TestAliveIndexesArePositions(t *testing.T) {
	side := api.Side{Team: []api.BattlePokemon{
		mon("a", 0, 20, true),
		mon("b", 20, 20, false),
		mon("c", 20, 20, false),
	}}
	got := aliveIndexes(side)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("aliveIndexes = %v, want [1 2] - positions, not a renumbering", got)
	}
}

func TestDefaultTargetSkipsTheFainted(t *testing.T) {
	side := api.Side{Team: []api.BattlePokemon{
		mon("a", 0, 20, true),
		mon("b", 20, 20, false),
	}}
	if got := defaultTarget(side); got != 1 {
		t.Errorf("defaultTarget = %d, want 1 - the living one", got)
	}
	if !onlyOneTarget(side) {
		t.Error("one living opponent should let the target tap be skipped")
	}
}

// On a phone the banner is the only always-visible place to say whose
// turn it is, so it must never be empty mid-battle.
func TestTurnBannerAlwaysSaysSomething(t *testing.T) {
	b := &api.Battle{Status: "active", Turn: api.NewOptString("misty")}
	if got := turnBanner(b, nil); !strings.Contains(got, "misty") {
		t.Errorf("banner = %q, want the opponent named", got)
	}
	b.Status = "finished"
	if got := turnBanner(b, nil); got != "battle over" {
		t.Errorf("finished banner = %q", got)
	}
}

// A phone shows fewer lines than a terminal; the log must trim rather
// than pushing the controls off screen.
func TestRecentLogTrimsToTheNewest(t *testing.T) {
	b := &api.Battle{}
	for i := 0; i < 10; i++ {
		b.Log = append(b.Log, api.BattleEvent{TurnNumber: i, Text: "e" + itoa(i)})
	}
	got := recentLog(b, 3)
	if len(got) != 3 || got[2].Text != "e9" {
		t.Errorf("recentLog kept %d entries ending %q, want 3 ending e9", len(got), got[len(got)-1].Text)
	}
	if n := len(recentLog(&api.Battle{}, 3)); n != 0 {
		t.Errorf("an empty log returned %d entries", n)
	}
}

// The three taps must be prompted in order, or a narrow screen shows
// three equally-live columns and the player guesses.
func TestPickStageNamesTheNextTap(t *testing.T) {
	var p pick
	if got := p.stage(); got != "pick a pokemon" {
		t.Errorf("first stage = %q", got)
	}
	p.haveAttacker = true
	if got := p.stage(); got != "pick a move" {
		t.Errorf("second stage = %q", got)
	}
	p.haveMove = true
	if got := p.stage(); got != "pick a target" {
		t.Errorf("third stage = %q", got)
	}
	if !p.ready() {
		t.Error("a full selection should be ready to send")
	}
	p.reset()
	if p.ready() || p.haveAttacker {
		t.Error("reset left state behind")
	}
}
