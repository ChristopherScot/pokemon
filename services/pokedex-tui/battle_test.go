package main

// The battle screen's logic, driven directly. Update is a pure function,
// so animation and cursor behaviour can be tested frame by frame without
// a terminal or a server.

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

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

// A bar must animate towards the new HP rather than snapping, and must
// arrive exactly - an interpolation that overshoots shows a Pokemon with
// less HP than the server says it has.
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

// Damage floats come from the log, because the log distinguishes one big
// hit from two small ones inside a single poll - an HP diff cannot.
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

// Floats expire, or they pile up over a long battle and never leave the
// screen.
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

// The cursor must never rest on a fainted Pokemon: selecting one and
// attacking is a guaranteed 409.
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

	// Cycling must stay on the living one rather than walking onto a
	// corpse.
	if got := nextAlive(team, 1); got != 1 {
		t.Errorf("nextAlive wrapped onto a fainted pokemon: %d", got)
	}
	if got := prevAlive(team, 1); got != 1 {
		t.Errorf("prevAlive wrapped onto a fainted pokemon: %d", got)
	}
}

// A poll that brings nothing new must not restart animations, or a
// static screen redraws forever and burns CPU.
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

// A styled column must be padded BY ITS STYLE, never by fmt.
//
// This is the bug, measured: fmt pads to byte count, and lipgloss styles
// wrap their content in escape sequences. A strikethrough "charmander"
// is 116 bytes for 12 visible columns, so %-24s considers it already
// over width and adds nothing - while an unstyled name in the same
// column pads normally. The rows then start at different places, which
// is exactly what the battle screen did to its fainted Pokemon.
//
//	%-24s of an unstyled name  -> 24 columns
//	%-24s of a styled name     -> 12 columns
//
// nameCol.Width(24) pads the visible text instead, so every row lines up
// whatever escapes it carries.
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

	// And the failure mode it replaces, so the reason this test exists
	// is visible rather than folklore.
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

// A hit flashes and shakes its row for a few frames, and the effect
// expires. An impact that never cleared would leave a row permanently
// mid-shake.
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

// The banner pulses when the turn becomes yours, and settles. A pulse
// that never stopped would flicker for the rest of the battle.
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

// An immune hit must not read as a hit that landed.
//
// renderFloat had no case for effect == 0, so a 0x hit fell through to
// the default hpDanger style and printed "-0" in damage red. logView,
// three functions away in this same file, already gave 0 its own case -
// so one event was styled two different ways on one screen. That is the
// same split the web had between its log and its floating number.
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
