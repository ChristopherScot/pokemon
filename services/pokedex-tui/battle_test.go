package main

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func mon(name string, hp, max int, fainted bool) api.BattlePokemon {
	return api.BattlePokemon{
		Name: name, Hp: hp, MaxHp: max, Fainted: fainted,
		Types: []string{"normal"},
		Moves: []api.Move{{Name: "tackle", Type: "normal", Power: 40}},
	}
}

func testBattleState(t *testing.T) *battleState {
	t.Helper()
	c, err := api.NewClient("http://127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bc := &battleclient.Client{API: c, Name: "ash", Token: "tok"}
	return &battleState{client: bc, id: "abc", shown: map[slot]int{}}
}

func twoSided(version int, mine, theirs []api.BattlePokemon) *api.Battle {
	return &api.Battle{
		ID: "abc", Status: api.BattleStatusActive, Version: version,
		Turn: api.NewOptString("ash"),
		Sides: []api.Side{
			{Trainer: "ash", Team: mine},
			{Trainer: "gary", Team: theirs},
		},
	}
}

func TestHPDrainsTowardsTheServerValueAndStops(t *testing.T) {
	bs := testBattleState(t)
	bs.applyBattle(twoSided(1,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 100, 100, false)}))

	// Server says the opponent took a big hit.
	bs.applyBattle(twoSided(2,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 40, 100, false)}))

	k := slot{1, 0}
	if bs.shown[k] != 100 {
		t.Fatalf("bar jumped immediately to %d; it should animate", bs.shown[k])
	}

	frames := 0
	for bs.advance() {
		frames++
		if frames > 200 {
			t.Fatal("drain never settled")
		}
	}
	if bs.shown[k] != 40 {
		t.Errorf("settled at %d, want exactly 40", bs.shown[k])
	}
	if frames == 0 {
		t.Error("no animation frames ran")
	}
}

func TestDamageFloatsComeFromTheLog(t *testing.T) {
	bs := testBattleState(t)
	first := twoSided(1,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 100, 100, false)})
	bs.applyBattle(first)

	second := twoSided(2,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 70, 100, false)})
	second.Log = []api.BattleEvent{{
		TurnNumber:    1,
		Text:          "Bulbasaur used Tackle on Pikachu!",
		Target:        api.NewOptString("pikachu"),
		Damage:        api.NewOptInt(30),
		Effectiveness: api.NewOptFloat64(2),
	}}
	bs.applyBattle(second)

	if len(bs.floats) != 1 {
		t.Fatalf("got %d floats, want 1", len(bs.floats))
	}
	if bs.floats[0].amount != 30 || bs.floats[0].effect != 2 {
		t.Errorf("float = %+v, want 30 damage at 2x", bs.floats[0])
	}
	if bs.floats[0].slot != (slot{1, 0}) {
		t.Errorf("float landed on %+v, want the opponent's first slot", bs.floats[0].slot)
	}
}

func TestFloatsExpire(t *testing.T) {
	bs := testBattleState(t)
	bs.applyBattle(twoSided(1,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 100, 100, false)}))
	bs.floats = append(bs.floats, damageFloat{slot: slot{1, 0}, amount: 10, life: 3})

	for i := 0; i < 5; i++ {
		bs.advance()
	}
	if len(bs.floats) != 0 {
		t.Errorf("%d floats survived past their life", len(bs.floats))
	}
}

func TestCursorSkipsFaintedPokemon(t *testing.T) {
	bs := testBattleState(t)
	team := []api.BattlePokemon{
		mon("a", 0, 100, true),
		mon("b", 50, 100, false),
		mon("c", 0, 100, true),
	}
	bs.applyBattle(twoSided(1, team, team))

	if got := bs.pickAttacker; got != 1 {
		t.Errorf("attacker cursor on %d, want the only living slot 1", got)
	}
	if got := bs.pickTarget; got != 1 {
		t.Errorf("target cursor on %d, want 1", got)
	}

	if got := nextPick(team, 1, battleclient.CanAct); got != 1 {
		t.Errorf("nextPick wrapped onto a fainted pokemon: %d", got)
	}
	if got := prevPick(team, 1, battleclient.CanAct); got != 1 {
		t.Errorf("prevPick wrapped onto a fainted pokemon: %d", got)
	}
}

func TestUnchangedPollAddsNoFloats(t *testing.T) {
	bs := testBattleState(t)
	b := twoSided(3,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 60, 100, false)})
	bs.applyBattle(b)
	for bs.advance() {
	}

	bs.applyBattle(b)
	if len(bs.floats) != 0 {
		t.Errorf("an unchanged poll produced %d floats", len(bs.floats))
	}
	if bs.advance() {
		t.Error("an unchanged poll restarted the animation loop")
	}
}

func TestStyledColumnsArePaddedByTheirStyleNotByFmt(t *testing.T) {
	for _, style := range []lipgloss.Style{
		lipgloss.NewStyle(),
		faintStyle,
		pickStyle,
	} {
		// What the code does: width on the style.
		if got := lipgloss.Width(nameCol.Inherit(style).Render("  charmander")); got != 24 {
			t.Errorf("style-padded column is %d wide, want 24", got)
		}
	}

	plain := lipgloss.Width(fmt.Sprintf("%-24s", "  charmander"))
	styled := lipgloss.Width(fmt.Sprintf("%-24s", faintStyle.Render("  charmander")))
	if plain == styled {
		t.Skip("fmt now measures display width; this guard is obsolete")
	}
	if styled >= plain {
		t.Errorf("expected fmt to under-pad a styled string: plain=%d styled=%d", plain, styled)
	}
}

// Every rendered row is the same width, whatever styling it carries.
func TestRowsAlignWhateverTheStyling(t *testing.T) {
	bs := testBattleState(t)
	team := []api.BattlePokemon{
		mon("mew", 100, 100, false),
		mon("charmander", 0, 100, true), // fainted: strikethrough
		mon("bulbasaur", 50, 100, false),
	}
	bs.applyBattle(twoSided(1, team, team))
	bs.focus = focusAttacker
	bs.pickAttacker = 2

	want := lipgloss.Width(bs.monLine(0, 0, team[0], false))
	for pi, p := range team {
		line := bs.monLine(0, pi, p, pi == bs.pickAttacker)
		if got := lipgloss.Width(line); got != want {
			t.Errorf("%s: row is %d wide, the reference row is %d", p.Name, got, want)
		}
	}
}

func TestImpactsFlashAndExpire(t *testing.T) {
	bs := testBattleState(t)
	bs.applyBattle(twoSided(1,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 100, 100, false)}))

	hit := twoSided(2,
		[]api.BattlePokemon{mon("bulbasaur", 100, 100, false)},
		[]api.BattlePokemon{mon("pikachu", 70, 100, false)})
	hit.Log = []api.BattleEvent{{
		TurnNumber: 1, Text: "hit",
		Target: api.NewOptString("pikachu"),
		Damage: api.NewOptInt(30), Effectiveness: api.NewOptFloat64(2),
	}}
	bs.applyBattle(hit)

	if len(bs.impacts) != 1 {
		t.Fatalf("got %d impacts, want 1", len(bs.impacts))
	}
	// The flashing row must actually differ from a calm one.
	flashing := bs.monLine(1, 0, hit.Sides[1].Team[0], false)
	for i := 0; i < impactLife+2; i++ {
		bs.advance()
	}
	if len(bs.impacts) != 0 {
		t.Errorf("%d impacts survived past their life", len(bs.impacts))
	}
	if calm := bs.monLine(1, 0, hit.Sides[1].Team[0], false); calm == flashing {
		t.Error("a flashing row renders identically to a calm one")
	}
}

func TestBannerPulsesWhenTheTurnArrives(t *testing.T) {
	bs := testBattleState(t)
	theirs := twoSided(1, nil, nil)
	theirs.Turn = api.NewOptString("gary")
	theirs.Sides = []api.Side{
		{Trainer: "ash", Team: []api.BattlePokemon{mon("a", 10, 10, false)}},
		{Trainer: "gary", Team: []api.BattlePokemon{mon("b", 10, 10, false)}},
	}
	bs.applyBattle(theirs)
	if bs.bannerPulse != 0 {
		t.Fatal("pulsing while it is not our turn")
	}

	mine := twoSided(2,
		[]api.BattlePokemon{mon("a", 10, 10, false)},
		[]api.BattlePokemon{mon("b", 10, 10, false)})
	bs.applyBattle(mine)
	if bs.bannerPulse == 0 {
		t.Error("the turn arrived and the banner did not pulse")
	}

	for i := 0; i < 40; i++ {
		bs.advance()
	}
	if bs.bannerPulse != 0 {
		t.Errorf("banner still pulsing after 40 frames: %d", bs.bannerPulse)
	}
}

func TestAnImmuneHitDoesNotFloatAsDamage(t *testing.T) {
	got := renderFloat(damageFloat{slot: slot{0, 0}, amount: 0, effect: 0, life: floatLife})
	if strings.Contains(got, "-0") {
		t.Errorf("immune float = %q, want it to say what happened rather than -0", got)
	}
	if !strings.Contains(got, "no effect") {
		t.Errorf("immune float = %q, want it to name the immunity", got)
	}
}

// The ordinary bands still read as they did.
func TestFloatsKeepTheirEffectivenessMarkers(t *testing.T) {
	super := renderFloat(damageFloat{slot: slot{0, 0}, amount: 30, effect: 2, life: floatLife})
	if !strings.Contains(super, "-30") || !strings.Contains(super, "!!") {
		t.Errorf("super float = %q, want the damage and its marker", super)
	}
	weak := renderFloat(damageFloat{slot: slot{0, 0}, amount: 3, effect: 0.5, life: floatLife})
	if !strings.Contains(weak, "-3") || !strings.Contains(weak, "...") {
		t.Errorf("weak float = %q, want the damage and its marker", weak)
	}
	normal := renderFloat(damageFloat{slot: slot{0, 0}, amount: 12, effect: 1, life: floatLife})
	if !strings.Contains(normal, "-12") {
		t.Errorf("normal float = %q, want the damage", normal)
	}
}

func TestEnterRefusesADisabledMove(t *testing.T) {
	bs := testBattleState(t)
	mine := mon("pikachu", 20, 20, false)
	mine.Moves = []api.Move{
		{Name: "tackle", Type: "normal", Power: 40},
		{Name: "swords-dance", Type: "normal"},
	}
	mine.DisabledMove = api.NewOptInt(1)

	b := twoSided(1, []api.BattlePokemon{mine}, []api.BattlePokemon{mon("staryu", 20, 20, false)})
	bs.battle = b
	bs.pickAttacker, bs.pickMove, bs.pickTarget = 0, 1, 0
	bs.focus = focusMove

	m := model{screen: screenBattle, battle: bs, bc: bs.client}
	got, cmd := m.battleKey(tea.KeyPressMsg{Code: '\r'})

	if cmd != nil {
		t.Error("enter sent a disabled move instead of refusing it")
	}
	if s := got.(model).status; s == "" {
		t.Error("refusing silently is worse than refusing: say why")
	} else if !strings.Contains(s, "disabled") {
		t.Errorf("status = %q, want it to name the reason", s)
	}
}

// An allowed move still goes through, or the guard is just a wall.
func TestEnterStillSendsALegalMove(t *testing.T) {
	bs := testBattleState(t)
	b := twoSided(1,
		[]api.BattlePokemon{mon("pikachu", 20, 20, false)},
		[]api.BattlePokemon{mon("staryu", 20, 20, false)})
	bs.battle = b
	bs.pickAttacker, bs.pickMove, bs.pickTarget = 0, 0, 0

	m := model{screen: screenBattle, battle: bs, bc: bs.client}
	if _, cmd := m.battleKey(tea.KeyPressMsg{Code: '\r'}); cmd == nil {
		t.Error("a legal move was refused")
	}
}
