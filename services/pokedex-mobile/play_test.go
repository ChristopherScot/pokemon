package main

// Playthroughs: the real UI, rendered headless, driven by taps.

import (
	"image/png"
	"os"
	"testing"

	"gioui.org/widget"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// The register screen is what a new phone shows, and it must actually
// draw - a blank first screen is the worst possible first impression
// and the easiest thing to ship by accident.
func TestRegisterScreenRenders(t *testing.T) {
	h := newHarness(t)
	h.ui.screen = screenRegister

	img := h.frame()
	if !notBlank(img) {
		t.Fatal("the register screen rendered nothing")
	}
	if os.Getenv("SHOTS") != "" {
		f, _ := os.Create("/tmp/shot-register.png")
		png.Encode(f, img)
		f.Close()
	}
}

// A battle in progress: the screen a player spends the most time on,
// and the one where a layout mistake costs a turn.
func TestBattleScreenRenders(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	bc := testClient(t)
	h.ui.bc = bc
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")

	img := h.frame()
	if !notBlank(img) {
		t.Fatal("the battle screen rendered nothing")
	}
	if os.Getenv("SHOTS") != "" {
		f, _ := os.Create("/tmp/shot-battle.png")
		png.Encode(f, img)
		f.Close()
	}
}

// A turn, played by tapping: pokemon, then move, then target.
//
// This is the flow that makes it a game rather than a screenshot, and
// the stages have to advance in order or a player is stuck.
func TestPlayATurnByTapping(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")
	h.frame()

	if h.ui.sel.stage() != "pick a pokemon" {
		t.Fatalf("opening stage = %q", h.ui.sel.stage())
	}

	h.tapOn("choose Pikachu")
	if !h.ui.sel.haveAttacker {
		t.Fatal("tapping a pokemon did not select it")
	}
	if h.ui.sel.stage() != "pick a move" {
		t.Fatalf("after picking a pokemon, stage = %q", h.ui.sel.stage())
	}

	// With two living opponents the selection would wait for a
	// target; the fixture has one, so tapping a move sends the turn
	// and clears the selection. Either way the tap must be OBSERVED -
	// a stage that does not advance is a player stuck on their turn.
	h.tapOn("choose Thunderbolt  ·  90")
	if h.ui.sel.haveAttacker && !h.ui.sel.haveMove {
		t.Fatal("tapping a move neither selected it nor sent the turn")
	}
}

// One living opponent means the target is not a choice, and asking for
// a third tap there is busywork the terminal does not impose either.
func TestOneTargetSkipsTheThirdTap(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	b := testBattle("ash", "misty")
	// psyduck is already fainted in the fixture, so staryu is alone.
	h.ui.battle = b
	h.frame()

	_, theirs, _ := h.ui.bc.SideFor(b)
	if !onlyOneTarget(theirs) {
		t.Fatal("fixture should leave exactly one living opponent")
	}

	h.tapOn("choose Pikachu")            // attacker
	h.tapOn("choose Thunderbolt  ·  90") // move -> should send immediately

	// Sending clears the selection, which is how we know it fired
	// rather than waiting for a target tap.
	if h.ui.sel.haveAttacker || h.ui.sel.haveMove {
		t.Error("the turn was not sent; it is still waiting for a target tap")
	}
}

// The Pokedex list, where a team gets picked. A long list on a phone
// must scroll by dragging, and picking must survive the scroll.
func TestBrowseScrollsAndPicks(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.screen = screenBrowse
	for i := 0; i < 40; i++ {
		h.ui.dex = append(h.ui.dex, api.Pokemon{
			ID: i + 1, Name: "mon" + itoa(i), Types: []string{"normal"},
		})
	}
	h.ui.dexClicks = make([]widget.Clickable, len(h.ui.dex))
	h.frame()

	// Tap the first row to pick it.
	h.tapOn("Mon0")
	if len(h.ui.team) == 0 {
		t.Fatal("tapping a row picked nothing")
	}
	picked := h.ui.team[0]

	// Drag upward to scroll down the list, the way a thumb does.
	before := h.ui.dexList.Position.First
	h.drag(phoneW/2, 600, 250)
	if h.ui.dexList.Position.First <= before {
		t.Errorf("dragging did not scroll: first went %d -> %d",
			before, h.ui.dexList.Position.First)
	}

	// The pick survives scrolling - it is state, not a screen position.
	if teamPosition(h.ui.team, picked) != 1 {
		t.Errorf("after scrolling, %q is no longer the lead pick", picked)
	}
	if os.Getenv("SHOTS") != "" {
		f, _ := os.Create("/tmp/shot-browse.png")
		png.Encode(f, h.img)
		f.Close()
	}
}

// The lobby and team screens, which are how a battle starts. A screen
// that renders nothing is a dead end a player cannot get out of.
func TestEveryScreenRenders(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*harness)
	}{
		{"register", func(h *harness) { h.ui.screen = screenRegister }},
		{"browse", func(h *harness) {
			h.ui.screen = screenBrowse
			h.ui.dex = []api.Pokemon{{ID: 1, Name: "pikachu", Types: []string{"electric"}}}
			h.ui.dexClicks = make([]widget.Clickable, 1)
		}},
		{"team-empty", func(h *harness) { h.ui.screen = screenTeam }},
		{"team-ready", func(h *harness) {
			h.ui.screen = screenTeam
			h.ui.team = []string{"pikachu", "geodude", "staryu"}
		}},
		{"lobby-empty", func(h *harness) { h.ui.screen = screenLobby }},
		{"lobby-full", func(h *harness) {
			h.ui.screen = screenLobby
			h.ui.lobby = &api.WaitingList{Count: 1, Waiting: []api.WaitingBattle{
				{BattleId: "b1", Trainer: "misty", Team: []string{"staryu"}},
			}}
			h.ui.lobbyBtns = make([]widget.Clickable, 1)
		}},
		{"battle-waiting", func(h *harness) {
			h.ui.bc = testClient(t)
			h.ui.screen = screenBattle
			b := testBattle("ash", "misty")
			b.Turn = api.NewOptString("misty") // not our turn
			h.ui.battle = b
		}},
		{"battle-finished", func(h *harness) {
			h.ui.bc = testClient(t)
			h.ui.screen = screenBattle
			b := testBattle("ash", "misty")
			b.Status = "finished"
			h.ui.battle = b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.ui.id.Name = "ash"
			tc.setup(h)
			img := h.frame()
			if !notBlank(img) {
				t.Fatalf("%s rendered nothing", tc.name)
			}
			if os.Getenv("SHOTS") != "" {
				f, _ := os.Create("/tmp/shot-" + tc.name + ".png")
				png.Encode(f, img)
				f.Close()
			}
		})
	}
}
