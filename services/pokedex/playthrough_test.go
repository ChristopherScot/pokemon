package main

import (
	"context"
	"math/rand"
	"testing"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// End-to-end game against Postgres: without it, JSON round-trip bugs
// (baseStats, stages) never surface because every other battle test
// keeps combatants in memory.
func TestAWholeGameOverPostgres(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)

	ctx := context.Background()
	if err := migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := seed(ctx, pool); err != nil {
		t.Fatal(err)
	}

	dex, err := loadPokedexFromDB(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	s := service{dex: dex, battles: newPGStore(pool), rng: rngFor(1)}

	ash, err := s.battles.registerTrainer(ctx, "ash")
	if err != nil {
		t.Fatal(err)
	}
	misty, err := s.battles.registerTrainer(ctx, "misty")
	if err != nil {
		t.Fatal(err)
	}

	opened, err := s.CreateBattle(ctx,
		&api.CreateBattle{Team: []string{"pikachu", "onix", "gengar"}},
		api.CreateBattleParams{XTrainerToken: ash})
	if err != nil {
		t.Fatal(err)
	}
	created, ok := opened.(*api.Battle)
	if !ok {
		t.Fatalf("opening a battle: %#v", opened)
	}

	joined, err := s.JoinBattle(ctx,
		&api.JoinBattle{Team: []string{"charizard", "blastoise", "venusaur"}},
		api.JoinBattleParams{ID: created.ID, XTrainerToken: misty})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := joined.(*api.Battle); !ok {
		t.Fatalf("joining: %#v", joined)
	}

	tokens := map[string]string{"ash": ash, "misty": misty}

	// A real game takes under twenty turns. Sixty is a broken-battle
	// signal, not a timeout to nudge.
	const maxTurns = 60
	var turns int
	var winner string
	pick := rand.New(rand.NewSource(7))

	for turns = 0; turns < maxTurns; turns++ {
		got, err := s.GetBattle(ctx, api.GetBattleParams{ID: created.ID})
		if err != nil {
			t.Fatal(err)
		}
		b, ok := got.(*api.Battle)
		if !ok {
			t.Fatalf("reading the battle: %#v", got)
		}
		if b.Status == "finished" {
			winner, _ = b.Winner.Get()
			break
		}

		turn, _ := b.Turn.Get()
		token, ok := tokens[turn]
		if !ok {
			t.Fatalf("turn %d: nobody's turn (%q)", turns, turn)
		}
		me := sideOf(b, turn)
		// Seeded but varied so the game exercises status moves,
		// switching and fainted targets - not just attack-with-best.
		attacker := chooseStanding(me, pick)
		target := chooseStanding(other(b, turn), pick)
		if attacker < 0 || target < 0 {
			t.Fatalf("turn %d: no one left to fight with", turns)
		}
		move := chooseMove(me.Team[attacker], pick)

		res, err := s.TakeTurn(ctx,
			&api.TakeTurn{Attacker: attacker, Move: move, Target: target},
			api.TakeTurnParams{ID: created.ID, XTrainerToken: token})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := res.(*api.Battle); !ok {
			t.Fatalf("turn %d rejected: %#v", turns, res)
		}
	}

	if winner == "" {
		t.Fatalf("no winner after %d turns: the battle is broken. "+
			"Do not raise this number - a working game ends in well under "+
			"twenty.", maxTurns)
	}
	t.Logf("%s won in %d turns", winner, turns)
}

func sideOf(b *api.Battle, trainer string) api.Side {
	for _, s := range b.Sides {
		if s.Trainer == trainer {
			return s
		}
	}
	return api.Side{}
}

func other(b *api.Battle, trainer string) api.Side {
	for _, s := range b.Sides {
		if s.Trainer != trainer {
			return s
		}
	}
	return api.Side{}
}

func chooseStanding(s api.Side, pick *rand.Rand) int {
	var up []int
	for i, m := range s.Team {
		if !m.Fainted {
			up = append(up, i)
		}
	}
	if len(up) == 0 {
		return -1
	}
	return up[pick.Intn(len(up))]
}

// chooseMove picks a legal move so a game plays out rather than the
// test probing validation.
func chooseMove(m api.BattlePokemon, pick *rand.Rand) int {
	disabled, hasDisabled := m.DisabledMove.Get()
	var usable []int
	for i := range m.Moves {
		if hasDisabled && i == disabled {
			continue
		}
		usable = append(usable, i)
	}
	if len(usable) == 0 {
		return 0
	}
	return usable[pick.Intn(len(usable))]
}
