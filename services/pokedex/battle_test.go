package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
	"github.com/christopherscot/pokemon/services/pokedex/battleclient"
)

func testService(t *testing.T) service {
	t.Helper()
	dex, err := loadPokedex()
	if err != nil {
		t.Fatal(err)
	}
	return service{dex: dex, battles: newMemStore(1), rng: rngFor(1)}
}

func activeBattle(t *testing.T, s service) (*battle, string, string) {
	t.Helper()
	a, err := s.battles.registerTrainer(context.Background(), "ash")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.battles.registerTrainer(context.Background(), "gary")
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

	stored, _ := s.battles.(*memStore).rawForTest(bt.ID)
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

func TestMaxHPFollowsTheRealFormula(t *testing.T) {
	for _, tc := range []struct {
		name string
		base int
		want int
	}{
		{"bulbasaur", 45, 105},
		{"charizard", 78, 138},
		{"wigglytuff", 140, 200}, // the bulkiest in this dataset
		{"diglett", 10, 70},      // the frailest
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := maxHP(tc.base, 0, 0, 50); got != tc.want {
				t.Errorf("maxHP(base=%d) = %d, want %d", tc.base, got, tc.want)
			}
		})
	}
}

func TestMaxHPDoesNotDivideBeforeMultiplying(t *testing.T) {
	const base, level = 45, 50
	got := maxHP(base, 0, 0, level)
	divideFirst := (2*base+0+0/4)/100*level + level + 10
	if got == divideFirst {
		t.Errorf("maxHP = %d, which matches the divide-first result; operator order is wrong", got)
	}
	if got != 105 {
		t.Errorf("maxHP = %d, want 105", got)
	}
}

func TestBulkFollowsBaseStatsNotWeight(t *testing.T) {
	s := testService(t)
	onix, ok := s.dex.get("onix")
	if !ok {
		t.Skip("onix not in this dataset")
	}
	jiggly, ok := s.dex.get("jigglypuff")
	if !ok {
		t.Skip("jigglypuff not in this dataset")
	}

	onixHP := maxHP(s.dex.stats[onix.ID].hp, 0, 0, battleLevel)
	jigglyHP := maxHP(s.dex.stats[jiggly.ID].hp, 0, 0, battleLevel)

	if onix.Weight <= jiggly.Weight {
		t.Fatalf("premise broken: onix %d should outweigh jigglypuff %d", onix.Weight, jiggly.Weight)
	}
	if onixHP >= jigglyHP {
		t.Errorf("onix %d HP >= jigglypuff %d HP; bulk is tracking weight rather than the base stat",
			onixHP, jigglyHP)
	}
}

func TestEveryPokemonHasABaseStat(t *testing.T) {
	s := testService(t)
	for _, mon := range s.dex.list("", 0) {
		if s.dex.stats[mon.ID].hp <= 0 {
			t.Errorf("#%d %s has no base HP", mon.ID, mon.Name)
		}
	}
}

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

func TestTrainerNamesAreUnique(t *testing.T) {
	s := testService(t)
	if _, err := s.battles.registerTrainer(context.Background(), "ash"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.battles.registerTrainer(context.Background(), "ASH"); !errors.Is(err, errNameTaken) {
		t.Errorf("case-different duplicate accepted: %v", err)
	}
}

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

func TestDamageRoundingAtTheEdges(t *testing.T) {
	s := testService(t)
	weakest, _ := s.dex.get("caterpie")
	tough, _ := s.dex.get("onix")
	att := &combatant{mon: weakest, hp: 100, maxHP: 100}
	def := &combatant{mon: tough, hp: 200, maxHP: 200}

	for _, mv := range weakest.Moves {
		if mv.Power == 0 {
			continue
		}
		for seed := int64(1); seed <= 50; seed++ {
			d, mult := damage(att, def, mv, rngFor(seed))
			if mult > 0 && d < 1 {
				t.Fatalf("%s at seed %d: %d damage at %vx", mv.Name, seed, d, mult)
			}
			if mult == 0 && d != 0 {
				t.Fatalf("%s at seed %d: %d damage against an immune target", mv.Name, seed, d)
			}
		}
	}
}

func TestRandomTeamIsThreeDistinctPokemon(t *testing.T) {
	s := testService(t)
	for seed := int64(1); seed <= 25; seed++ {
		team := randomTeam(s.dex, rngFor(seed))
		if len(team) != teamSize {
			t.Fatalf("seed %d: got %d pokemon, want %d", seed, len(team), teamSize)
		}
		seen := map[string]bool{}
		for _, n := range team {
			if seen[n] {
				t.Errorf("seed %d: %s appears twice in %v", seed, n, team)
			}
			seen[n] = true
			if _, ok := s.dex.get(n); !ok {
				t.Errorf("seed %d: %q is not in the pokedex", seed, n)
			}
		}
	}
}

func TestCreateWithNoTeamPicksOne(t *testing.T) {
	s := testService(t)
	tok, err := s.battles.registerTrainer(context.Background(), "ash")
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.CreateBattle(t.Context(), &api.CreateBattle{},
		api.CreateBattleParams{XTrainerToken: tok})
	if err != nil {
		t.Fatal(err)
	}
	b, ok := res.(*api.Battle)
	if !ok {
		t.Fatalf("got %#v, want a battle", res)
	}
	if n := len(b.Sides[0].Team); n != teamSize {
		t.Fatalf("random team has %d pokemon, want %d", n, teamSize)
	}
	for _, p := range b.Sides[0].Team {
		if p.MaxHp <= 0 || p.Hp != p.MaxHp {
			t.Errorf("%s started at %d/%d", p.Name, p.Hp, p.MaxHp)
		}
	}
}

func TestTokensDoNotFollowTheSeed(t *testing.T) {
	a := newMemStore(42)
	b := newMemStore(42)

	ta, err := a.registerTrainer(context.Background(), "ash")
	if err != nil {
		t.Fatal(err)
	}
	tb, err := b.registerTrainer(context.Background(), "ash")
	if err != nil {
		t.Fatal(err)
	}
	if ta == tb {
		t.Error("two stores with the same seed minted the same token; it is derived from the seed")
	}

	// And tokens within one store differ from each other.
	seen := map[string]bool{ta: true}
	for i := 0; i < 50; i++ {
		tok, err := a.registerTrainer(context.Background(), fmt.Sprintf("trainer-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatalf("token %q repeated", tok)
		}
		seen[tok] = true
		if len(tok) != 24 {
			t.Errorf("token %q is %d characters, want 24", tok, len(tok))
		}
	}
}

func TestJoiningWithAnUnknownPokemonIsABadRequest(t *testing.T) {
	s := testService(t)
	ctx := context.Background()

	host, err := s.battles.registerTrainer(ctx, "ash")
	if err != nil {
		t.Fatal(err)
	}
	joiner, err := s.battles.registerTrainer(ctx, "gary")
	if err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateBattle(ctx, &api.CreateBattle{}, api.CreateBattleParams{XTrainerToken: host})
	if err != nil {
		t.Fatal(err)
	}
	b := created.(*api.Battle)

	res, err := s.JoinBattle(ctx, &api.JoinBattle{Team: []string{"missingno"}}, api.JoinBattleParams{
		ID: b.ID, XTrainerToken: joiner,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad, ok := res.(*api.JoinBattleBadRequest)
	if !ok {
		t.Fatalf("JoinBattle returned %T, want *api.JoinBattleBadRequest", res)
	}
	if !strings.Contains(bad.Message, "missingno") {
		t.Errorf("message = %q, want it to name the pokemon that was wrong", bad.Message)
	}
}

func TestClientTurnCheckAgreesWithTheServer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		turn  battleclient.Turn
		mut   func(*battle)
		legal bool
	}{
		{"a legal turn", battleclient.Turn{Attacker: 0, Move: 0, Target: 0}, nil, true},
		{"no such attacker", battleclient.Turn{Attacker: 7, Move: 0, Target: 0}, nil, false},
		{"no such target", battleclient.Turn{Attacker: 0, Move: 0, Target: 7}, nil, false},
		{"no such move", battleclient.Turn{Attacker: 0, Move: 7, Target: 0}, nil, false},
		{"attacker fainted", battleclient.Turn{Attacker: 0, Move: 0, Target: 0},
			func(b *battle) { b.sides[0].team[0].hp = 0 }, false},
		{"target fainted", battleclient.Turn{Attacker: 0, Move: 0, Target: 0},
			func(b *battle) { b.sides[1].team[0].hp = 0 }, false},
		{"move disabled", battleclient.Turn{Attacker: 0, Move: 1, Target: 0},
			func(b *battle) { b.sides[0].team[0].disabled = 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := testService(t)
			b, tokenA, _ := activeBattle(t, s)
			if tc.mut != nil {
				tc.mut(b)
			}

			before := b.toAPI()
			c := &battleclient.Client{Name: b.sides[0].trainer}
			clientErr := c.CheckTurn(before, tc.turn)

			// What the authority says, from the real engine.
			serverErr := b.takeTurn(tokenA, tc.turn.Attacker, tc.turn.Move, tc.turn.Target, s.rng)

			if tc.legal {
				if serverErr != nil {
					t.Fatalf("the scenario is wrong: the server refused it with %v", serverErr)
				}
				if clientErr != nil {
					t.Errorf("the client refused a turn the server allowed: %v", clientErr)
				}
				return
			}

			if serverErr == nil {
				t.Fatalf("the scenario is wrong: the server allowed it")
			}
			if clientErr == nil {
				t.Errorf("the client allowed a turn the server refused with %q", serverErr)
			}
		})
	}
}
