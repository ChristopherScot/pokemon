package main

import (
	"context"
	"testing"
	"time"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// A name nobody has used in a long time goes back in the pool.
//
// Names are unique and nothing released them, so every name ever typed
// was spent forever - including by whoever lost their token, who could
// neither re-register it nor recover it.
func TestAnAbandonedNameCanBeClaimedAgain(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()

	if _, err := m.registerTrainer(ctx, "ash"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.registerTrainer(ctx, "ash"); err == nil {
		t.Fatal("the name should be taken while it is in use")
	}

	// Nobody has been "ash" in a fortnight.
	m.mu.Lock()
	for token := range m.lastSeen {
		m.lastSeen[token] = time.Now().Add(-2 * trainerTTL)
	}
	m.sweepLocked()
	m.mu.Unlock()

	if _, err := m.registerTrainer(ctx, "ash"); err != nil {
		t.Errorf("an abandoned name should be claimable again: %v", err)
	}
}

// The constraint that matters: a trainer IN a battle is never swept,
// however idle they look. Someone waiting in the lobby for an opponent
// makes no requests at all, and deleting them cascades their side away
// and strands whoever eventually joins.
func TestASweepNeverStrandsSomeoneInABattle(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()

	waiting, err := m.registerTrainer(ctx, "waiting-for-a-game")
	if err != nil {
		t.Fatal(err)
	}
	dex, err := loadPokedex()
	if err != nil {
		t.Fatal(err)
	}
	team, err := newCombatants(dex, []string{"pikachu", "onix", "gengar"})
	if err != nil {
		t.Fatal(err)
	}
	b := &battle{
		id: "open", status: "waiting", touched: time.Now(),
		sides: []*side{{trainer: "waiting-for-a-game", token: waiting, team: team}},
	}
	if err := m.create(ctx, b); err != nil {
		t.Fatal(err)
	}

	// They have been sitting in the lobby far longer than the TTL,
	// because sitting in a lobby involves making no requests.
	m.mu.Lock()
	m.lastSeen[waiting] = time.Now().Add(-10 * trainerTTL)
	m.sweepLocked()
	_, stillATrainer := m.trainers[waiting]
	m.mu.Unlock()

	if !stillATrainer {
		t.Error("swept a trainer who was waiting in a battle; their opponent would join a game with one side")
	}
	if name, err := m.trainerByToken(ctx, waiting); err != nil || name != "waiting-for-a-game" {
		t.Errorf("their token stopped working mid-game: %v", err)
	}
}

// Acting refreshes the claim, so a regular player never loses a name.
func TestUsingATokenKeepsTheName(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()

	token, err := m.registerTrainer(ctx, "regular")
	if err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	m.lastSeen[token] = time.Now().Add(-2 * trainerTTL)
	m.mu.Unlock()

	// One request - which is what every authenticated call does.
	if _, err := m.trainerByToken(ctx, token); err != nil {
		t.Fatalf("token stopped working: %v", err)
	}

	m.mu.Lock()
	m.sweepLocked()
	_, kept := m.trainers[token]
	m.mu.Unlock()

	if !kept {
		t.Error("a trainer who just made a request was swept anyway")
	}
}

// The same two rules, against postgres, because the NOT IN guard is SQL
// and the memory store's is Go - one passing says nothing about the
// other.
func TestSweepOverPostgres(t *testing.T) {
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

	idle, err := s.battles.registerTrainer(ctx, "abandoned")
	if err != nil {
		t.Fatal(err)
	}
	inGame, err := s.battles.registerTrainer(ctx, "mid-game")
	if err != nil {
		t.Fatal(err)
	}

	// mid-game opens a battle and then goes quiet, as anyone waiting
	// for an opponent does.
	if _, err := s.CreateBattle(ctx,
		&api.CreateBattle{Team: []string{"pikachu", "onix", "gengar"}},
		api.CreateBattleParams{XTrainerToken: inGame}); err != nil {
		t.Fatal(err)
	}

	// Both look ancient. The battle does not, so it is not swept.
	if _, err := pool.Exec(ctx, `UPDATE trainers SET last_seen = now() - interval '30 days'`); err != nil {
		t.Fatal(err)
	}

	// waiting() is where the sweep runs.
	s.battles.waiting(ctx)

	if _, err := s.battles.trainerByToken(ctx, inGame); err != nil {
		t.Errorf("swept a trainer who was waiting in a battle; the opponent would join a one-sided game: %v", err)
	}
	if _, err := s.battles.trainerByToken(ctx, idle); err == nil {
		t.Error("kept a trainer nobody has been in 30 days")
	}
	if _, err := s.battles.registerTrainer(ctx, "abandoned"); err != nil {
		t.Errorf("the abandoned name should be free again: %v", err)
	}
	if _, err := s.battles.registerTrainer(ctx, "mid-game"); err == nil {
		t.Error("took the name of someone who is still in a battle")
	}
}
