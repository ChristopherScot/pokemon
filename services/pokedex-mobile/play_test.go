package main

import (
	"strconv"
	"strings"
	"testing"

	"gioui.org/widget"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func TestRegisterScreenRenders(t *testing.T) {
	h := newHarness(t)
	h.ui.screen = screenRegister

	img := h.frame()
	if !notBlank(img) {
		t.Fatal("the register screen rendered nothing")
	}
	h.shot("register")
}

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
	h.shot("battle")
}

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

	// Fixture leaves one living opponent, so tapping a move sends the turn; assert what was sent, not just that something was.
	h.tapOn("choose Thunderbolt  ·  90")
	want := pick{attacker: 0, move: 0, target: 0, haveAttacker: true, haveMove: true}
	if h.ui.lastSent != want {
		t.Errorf("sent turn = %+v, want %+v", h.ui.lastSent, want)
	}
	if h.ui.sel.haveAttacker && !h.ui.sel.haveMove {
		t.Fatal("tapping a move neither selected it nor sent the turn")
	}
}

func TestOneTargetSkipsTheThirdTap(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	b := testBattle("ash", "misty")
	h.ui.battle = b
	h.frame()

	_, theirs, _ := h.ui.bc.SideFor(b)
	if !onlyOneTarget(theirs) {
		t.Fatal("fixture should leave exactly one living opponent")
	}

	h.tapOn("choose Pikachu")
	h.tapOn("choose Thunderbolt  ·  90")

	if h.ui.sel.haveAttacker || h.ui.sel.haveMove {
		t.Error("the turn was not sent; it is still waiting for a target tap")
	}
}

// A long list on a phone must scroll by dragging, and picking must survive the scroll.
func TestBrowseScrollsAndPicks(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.screen = screenBrowse
	for i := 0; i < 40; i++ {
		h.ui.dex = append(h.ui.dex, api.Pokemon{
			ID: i + 1, Name: "mon" + strconv.Itoa(i), Types: []string{"normal"},
		})
	}
	h.ui.dexClicks = make([]widget.Clickable, len(h.ui.dex))
	h.frame()

	h.tapOn("Mon0")
	if len(h.ui.team) == 0 {
		t.Fatal("tapping a row picked nothing")
	}
	picked := h.ui.team[0]

	before := h.ui.dexList.Position.First
	h.drag(phoneW/2, 600, 250)
	if h.ui.dexList.Position.First <= before {
		t.Errorf("dragging did not scroll: first went %d -> %d",
			before, h.ui.dexList.Position.First)
	}

	if teamPosition(h.ui.team, picked) != 1 {
		t.Errorf("after scrolling, %q is no longer the lead pick", picked)
	}
	h.shot("browse")
}

// Every screen asserted on the controls it must offer; a bare "not all one colour" check passes for a screen whose body drew nothing.
func TestEveryScreenRenders(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, *harness)
		want  []string
	}{
		{"register", func(t *testing.T, h *harness) {
			h.ui.screen = screenRegister
		}, []string{"Register"}},

		{"browse", func(t *testing.T, h *harness) {
			h.ui.screen = screenBrowse
			h.ui.dex = []api.Pokemon{{ID: 1, Name: "pikachu", Types: []string{"electric"}}}
			h.ui.dexClicks = make([]widget.Clickable, 1)
		}, []string{"Pikachu", "Pokedex", "Lobby"}},

		{"team-empty", func(t *testing.T, h *harness) {
			h.ui.screen = screenTeam
		}, []string{"Start battle", "Back to Pokedex"}},

		{"team-ready", func(t *testing.T, h *harness) {
			h.ui.screen = screenTeam
			h.ui.team = []string{"pikachu", "geodude", "staryu"}
		}, []string{"Start battle"}},

		{"lobby-empty", func(t *testing.T, h *harness) {
			h.ui.screen = screenLobby
		}, []string{"Open a new battle", "Refresh"}},

		{"lobby-full", func(t *testing.T, h *harness) {
			h.ui.screen = screenLobby
			h.ui.lobby = &api.WaitingList{Count: 1, Waiting: []api.WaitingBattle{
				{BattleId: "b1", Trainer: "misty", Team: []string{"staryu"}},
			}}
			h.ui.lobbyBtns = make([]widget.Clickable, 1)
		}, []string{"Misty", "Open a new battle"}},

		{"battle-waiting", func(t *testing.T, h *harness) {
			h.ui.bc = testClient(t)
			h.ui.screen = screenBattle
			b := testBattle("ash", "misty")
			b.Turn = api.NewOptString("misty")
			h.ui.battle = b
		}, []string{"Leave"}},

		{"battle-finished", func(t *testing.T, h *harness) {
			h.ui.bc = testClient(t)
			h.ui.screen = screenBattle
			b := testBattle("ash", "misty")
			b.Status = "finished"
			b.Winner = api.NewOptString("ash")
			h.ui.battle = b
		}, []string{"Back to lobby"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.ui.id.Name = "ash"
			tc.setup(t, h)
			img := h.frame()
			if !notBlank(img) {
				t.Fatalf("%s rendered nothing at all", tc.name)
			}
			for _, w := range tc.want {
				if _, ok := h.find(w); !ok {
					t.Errorf("%s: no %q on screen; visible: %v", tc.name, w, h.visible())
				}
			}
			h.shot(tc.name)
		})
	}
}

// A player on the other trainer's turn must not be offered moves; the server answers one with a 409.
func TestWaitingOffersNoMoves(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	b := testBattle("ash", "misty")
	b.Turn = api.NewOptString("misty")
	h.ui.battle = b
	h.frame()

	for _, v := range h.visible() {
		if strings.HasPrefix(v, "choose ") {
			t.Errorf("offered %q while waiting on the opponent", v)
		}
	}
}

func TestBackUndoesAStage(t *testing.T) {
	h := newHarness(t)
	h.ui.id.Name = "ash"
	h.ui.bc = testClient(t)
	h.ui.screen = screenBattle
	h.ui.battle = testBattle("ash", "misty")
	h.frame()

	h.tapOn("choose Pikachu")
	if !h.ui.sel.haveAttacker {
		t.Fatal("tapping an attacker did not select it")
	}
	h.tapOn("Back")
	if h.ui.sel.haveAttacker {
		t.Error("Back did not undo the attacker choice")
	}
	if _, ok := h.find("choose Pikachu"); !ok {
		t.Error("after Back, the attacker options are not offered again")
	}
}
