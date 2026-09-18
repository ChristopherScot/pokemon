package main

// The engine decides every rule, so these tests are where battle
// correctness is established. They drive the engine directly rather than
// through HTTP: the handlers are thin, and a rule is easier to pin down
// without a transport in the way.

import (
	"errors"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

func testService(t *testing.T) service {
	t.Helper()
	dex, err := loadPokedex()
	if err != nil {
		t.Fatal(err)
	}
	// Fixed seed: a damage roll that varies run to run makes a failing
	// test impossible to reproduce.
	return service{dex: dex, battles: newMemStore(1), rng: rngFor(1)}
}

// activeBattle sets up two trainers mid-fight, which is the starting
// point for most of what follows.
func activeBattle(t *testing.T, s service) (*battle, string, string) {
	t.Helper()
	a, err := s.battles.registerTrainer("ash")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.battles.registerTrainer("gary")
	if err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	created, err := s.CreateBattle(ctx, &api.CreateBattle{
		Team: []string{"bulbasaur", "charmander", "squirtle"},
	}, api.CreateBattleParams{XTrainerToken: a})
	if err != nil {
		t.Fatal(err)
	}
	bt, ok := created.(*api.Battle)
	if !ok {
		t.Fatalf("create: %#v", created)
	}
	if _, err := s.JoinBattle(ctx, &api.JoinBattle{
		Team: []string{"pikachu", "geodude", "pidgey"},
	}, api.JoinBattleParams{ID: bt.ID, XTrainerToken: b}); err != nil {
		t.Fatal(err)
	}

	stored, _ := s.battles.get(bt.ID)
	return stored, a, b
}

// Type effectiveness is the one rule players will check by hand.
func TestTypeMultipliers(t *testing.T) {
	for _, tc := range []struct {
		move  string
		types []string
		want  float64
	}{
		{"water", []string{"fire"}, 2},
		{"water", []string{"grass"}, 0.5},
		{"electric", []string{"ground"}, 0},
		{"normal", []string{"ghost"}, 0},
		// Dual types multiply, which is what makes some matchups brutal.
		{"electric", []string{"water", "flying"}, 4},
		{"grass", []string{"fire", "flying"}, 0.25},
		{"fire", []string{"normal"}, 1},
		// A type the chart does not list must not panic or zero out.
		{"mystery", []string{"fire"}, 1},
	} {
		if got := multiplier(tc.move, tc.types); got != tc.want {
			t.Errorf("multiplier(%q, %v) = %v, want %v", tc.move, tc.types, got, tc.want)
		}
	}
}

// HP has to be derived, bounded, and identical for the same Pokemon
// every time - a client shows this number too.
func TestMaxHPIsBoundedAndDeterministic(t *testing.T) {
	s := testService(t)
	for _, name := range []string{"pidgey", "snorlax", "bulbasaur", "onix"} {
		mon, ok := s.dex.get(name)
		if !ok {
			continue
		}
		hp := maxHP(mon)
		if hp < 90 || hp > 220 {
			t.Errorf("%s: hp %d outside the clamp", name, hp)
		}
		if again := maxHP(mon); again != hp {
			t.Errorf("%s: hp not deterministic: %d then %d", name, hp, again)
		}
	}
}

// A move that connects always does something. Rounding to zero reads as
// a bug to the player.
func TestDamageNeverRoundsToZeroWhenItConnects(t *testing.T) {
	s := testService(t)
	weak, _ := s.dex.get("pidgey")
	tough, _ := s.dex.get("onix")
	att := &combatant{mon: weak, hp: 100, maxHP: 100}
	def := &combatant{mon: tough, hp: 200, maxHP: 200}

	for _, mv := range weak.Moves {
		if mv.Power == 0 {
			continue
		}
		d, mult := damage(att, def, mv, rngFor(7))
		if mult > 0 && d < 1 {
			t.Errorf("%s did %d damage at %vx", mv.Name, d, mult)
		}
	}
}

// An immune matchup deals nothing, and the narration says so rather than
// reporting a hit for zero.
func TestImmuneMatchupDealsNothing(t *testing.T) {
	s := testService(t)
	pikachu, _ := s.dex.get("pikachu")
	geodude, _ := s.dex.get("geodude") // ground: immune to electric

	var electric api.Move
	for _, mv := range pikachu.Moves {
		if mv.Type == "electric" && mv.Power > 0 {
			electric = mv
			break
		}
	}
	if electric.Name == "" {
		t.Skip("pikachu has no damaging electric move in this dataset")
	}

	att := &combatant{mon: pikachu, hp: 100, maxHP: 100}
	def := &combatant{mon: geodude, hp: 150, maxHP: 150}
	d, mult := damage(att, def, electric, rngFor(3))
	if mult != 0 || d != 0 {
		t.Errorf("electric on ground: %d damage at %vx, want 0 at 0x", d, mult)
	}
}

// Turn order alternates, and a player cannot move twice.
func TestTurnsAlternate(t *testing.T) {
	s := testService(t)
	b, ash, gary := activeBattle(t, s)

	if err := b.takeTurn(ash, 0, 0, 0, s.rng); err != nil {
		t.Fatalf("ash's first turn: %v", err)
	}
	if err := b.takeTurn(ash, 0, 0, 0, s.rng); !errors.Is(err, errNotYourTurn) {
		t.Errorf("ash moved twice: %v", err)
	}
	if err := b.takeTurn(gary, 0, 0, 0, s.rng); err != nil {
		t.Errorf("gary's turn rejected: %v", err)
	}
}

// Every illegal index is an error, not a silent no-op: a client bug
// should be visible rather than looking like lag.
func TestIllegalMovesAreRejected(t *testing.T) {
	s := testService(t)
	b, ash, _ := activeBattle(t, s)

	for _, tc := range []struct {
		name                   string
		attacker, move, target int
	}{
		{"attacker out of range", 9, 0, 0},
		{"negative attacker", -1, 0, 0},
		{"target out of range", 0, 0, 9},
		{"move out of range", 0, 99, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := b.takeTurn(ash, tc.attacker, tc.move, tc.target, s.rng); !errors.Is(err, errIllegalMove) {
				t.Errorf("got %v, want errIllegalMove", err)
			}
		})
	}
}

// A fainted Pokemon cannot attack and cannot be attacked. Both would
// otherwise let a player waste turns or pile damage onto a corpse.
func TestFaintedPokemonAreOutOfPlay(t *testing.T) {
	s := testService(t)
	b, ash, gary := activeBattle(t, s)

	b.sides[1].team[0].hp = 0
	if err := b.takeTurn(ash, 0, 0, 0, s.rng); !errors.Is(err, errTargetFainted) {
		t.Errorf("attacking a fainted target: %v", err)
	}

	// And as an attacker, on the other side's turn.
	if err := b.takeTurn(ash, 0, 0, 1, s.rng); err != nil {
		t.Fatal(err)
	}
	b.sides[1].team[0].hp = 0
	if err := b.takeTurn(gary, 0, 0, 0, s.rng); !errors.Is(err, errIllegalMove) {
		t.Errorf("attacking with a fainted pokemon: %v", err)
	}
}

// Knocking out all three ends the battle, names a winner, and stops
// accepting turns.
func TestBattleEndsWhenATeamIsWiped(t *testing.T) {
	s := testService(t)
	b, ash, _ := activeBattle(t, s)

	// Leave one opponent standing on 1 HP; anything that connects ends it.
	b.sides[1].team[1].hp = 0
	b.sides[1].team[2].hp = 0
	b.sides[1].team[0].hp = 1

	if err := b.takeTurn(ash, 0, 0, 0, s.rng); err != nil {
		t.Fatal(err)
	}
	if b.status != "finished" {
		t.Fatalf("status = %q, want finished", b.status)
	}
	if b.winner != "ash" {
		t.Errorf("winner = %q, want ash", b.winner)
	}
	if err := b.takeTurn(ash, 0, 0, 0, s.rng); !errors.Is(err, errBattleOver) {
		t.Errorf("accepted a turn after the win: %v", err)
	}

	last := b.log[len(b.log)-1]
	if !strings.Contains(last.Text, "wins") {
		t.Errorf("log does not announce the win: %q", last.Text)
	}
}

// version increases on every change, because that is what clients poll
// on. If it stalls, a client renders stale state forever.
func TestVersionAdvancesOnEveryChange(t *testing.T) {
	s := testService(t)
	b, ash, gary := activeBattle(t, s)

	seen := b.version
	for i, token := range []string{ash, gary, ash} {
		if err := b.takeTurn(token, 0, 0, 0, s.rng); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if b.version <= seen {
			t.Fatalf("turn %d: version %d did not advance past %d", i, b.version, seen)
		}
		seen = b.version
	}
}

// A rejected turn must not advance anything: a client that retries after
// a 409 would otherwise skip a turn it never took.
func TestRejectedTurnChangesNothing(t *testing.T) {
	s := testService(t)
	b, _, gary := activeBattle(t, s)

	before, hp, turn := b.version, b.sides[1].team[0].hp, b.turn
	if err := b.takeTurn(gary, 0, 0, 0, s.rng); !errors.Is(err, errNotYourTurn) {
		t.Fatalf("expected a rejection, got %v", err)
	}
	if b.version != before || b.sides[1].team[0].hp != hp || b.turn != turn {
		t.Error("a rejected turn mutated the battle")
	}
}

// Names are first-come. Without this two players share an identity and
// the lobby is meaningless.
func TestTrainerNamesAreUnique(t *testing.T) {
	s := testService(t)
	if _, err := s.battles.registerTrainer("ash"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.battles.registerTrainer("ASH"); !errors.Is(err, errNameTaken) {
		t.Errorf("case-different duplicate accepted: %v", err)
	}
}

// The wire type must not carry tokens. Leaking one lets anyone move that
// trainer's Pokemon.
func TestAPIViewNeverLeaksTokens(t *testing.T) {
	s := testService(t)
	b, ash, gary := activeBattle(t, s)

	out := b.toAPI()
	for _, side := range out.Sides {
		if side.Trainer == ash || side.Trainer == gary {
			t.Error("a trainer token appears where a name belongs")
		}
	}
	// And nothing in the rendered log should contain one either.
	for _, ev := range out.Log {
		if strings.Contains(ev.Text, ash) || strings.Contains(ev.Text, gary) {
			t.Errorf("token leaked into the log: %q", ev.Text)
		}
	}
}
