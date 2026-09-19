package main

// The Postgres store, against a real database. Skipped without
// POKEDEX_TEST_DSN.
//
// These are the tests that justify the whole change: state surviving a
// restart, and two replicas not corrupting a battle between them.

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/christopherscot/pokemon/services/pokedex/api"
)

// freshPG returns a migrated, seeded database and a store on it.
func freshPG(t *testing.T) (*pgxpool.Pool, *pgStore, *pokedex) {
	t.Helper()
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()
	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := seed(ctx, pool); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dex, err := loadPokedexFromDB(ctx, pool)
	if err != nil {
		t.Fatalf("loading pokedex: %v", err)
	}
	return pool, newPGStore(pool, 1), dex
}

func TestPGRegisterTrainer(t *testing.T) {
	_, store, _ := freshPG(t)

	token, err := store.registerTrainer("Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	name, ok := store.trainerByToken(token)
	if !ok || name != "Ash" {
		t.Fatalf("trainerByToken = %q, %v; want Ash, true", name, ok)
	}

	// Case-insensitive, matching the in-memory store.
	if _, err := store.registerTrainer("ash"); !errors.Is(err, errNameTaken) {
		t.Errorf("registering ash after Ash = %v, want errNameTaken", err)
	}
}

// The race the UNIQUE index exists for: many clients claiming one name
// at the same moment. Exactly one may win.
func TestPGRegisterTrainerRace(t *testing.T) {
	_, store, _ := freshPG(t)

	const clients = 8
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		wins   int
		taken  int
		others []error
	)
	for range clients {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.registerTrainer("misty")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, errNameTaken):
				taken++
			default:
				others = append(others, err)
			}
		}()
	}
	wg.Wait()

	if len(others) > 0 {
		t.Fatalf("unexpected errors: %v", others)
	}
	if wins != 1 {
		t.Errorf("%d clients registered the same name, want exactly 1", wins)
	}
	if taken != clients-1 {
		t.Errorf("%d got errNameTaken, want %d", taken, clients-1)
	}
}

// A battle written by one store is readable by another - which is what
// "survives a restart" and "two replicas" both come down to.
func TestPGBattleSurvivesANewStore(t *testing.T) {
	pool, store, dex := freshPG(t)

	token, err := store.registerTrainer("Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(b)

	// A different store on the same database: a second replica, or the
	// same one after a restart.
	other := newPGStore(pool, 2)
	got, ok := other.get(b.id)
	if !ok {
		t.Fatalf("battle %s not found by a second store", b.id)
	}
	// Asserted on the API view, which is what get() now returns: the
	// store converts under its own lock so nothing reachable from it
	// escapes.
	if string(got.Status) != b.status || len(got.Sides) != len(b.sides) {
		t.Errorf("read back status %q with %d sides, want %q with %d",
			got.Status, len(got.Sides), b.status, len(b.sides))
	}
	if got.Sides[0].Trainer != "Ash" {
		t.Errorf("side 0 trainer %q, want Ash", got.Sides[0].Trainer)
	}
	if len(got.Sides[0].Team) != len(b.sides[0].team) {
		t.Errorf("team of %d, want %d", len(got.Sides[0].Team), len(b.sides[0].team))
	}
	// HP round-trips through JSONB rather than being recomputed.
	if got.Sides[0].Team[0].Hp != b.sides[0].team[0].hp {
		t.Errorf("hp %d, want %d", got.Sides[0].Team[0].Hp, b.sides[0].team[0].hp)
	}
}

// The heart of it: concurrent updates to one battle must serialise, so
// every mutation lands. Without SERIALIZABLE and the retry, some of
// these overwrite each other and the count comes up short.
func TestPGConcurrentUpdatesAllLand(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer("Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(b)

	// Each writer appends one event. If two interleave and one is
	// lost, the log is short - a silent corruption, which is exactly
	// the failure mode this design exists to prevent.
	const writers = 10
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.update(b.id, func(cur *battle) error {
				cur.log = append(cur.log, api.BattleEvent{
					TurnNumber: len(cur.log),
					Text:       "event",
				})
				return nil
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}

	got, ok := store.get(b.id)
	if !ok {
		t.Fatal("battle disappeared")
	}
	if len(got.Log) != writers {
		t.Errorf("log has %d events after %d concurrent writers, want %d - "+
			"a write was lost", len(got.Log), writers, writers)
	}
	// version moves once per successful update, which is what clients
	// poll on.
	if got.Version < writers {
		t.Errorf("version is %d after %d updates, want at least %d",
			got.Version, writers, writers)
	}
}

// An error from the closure rolls back and is returned as-is, rather
// than being retried or swallowed.
func TestPGUpdatePropagatesClosureError(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer("Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(b)

	sentinel := errors.New("nope")
	err = store.update(b.id, func(cur *battle) error {
		cur.status = "finished"
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("update returned %v, want the closure's error", err)
	}

	got, _ := store.get(b.id)
	if string(got.Status) == "finished" {
		t.Error("the mutation was committed despite the closure failing")
	}
}

func TestPGUpdateMissingBattle(t *testing.T) {
	_, store, _ := freshPG(t)
	err := store.update("nosuchid", func(*battle) error { return nil })
	if !errors.Is(err, errNoBattle) {
		t.Errorf("update on a missing battle = %v, want errNoBattle", err)
	}
}

func TestPGWaitingLists(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer("Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(b)

	waiting := store.waiting()
	if len(waiting) != 1 || waiting[0].BattleId != b.id {
		t.Fatalf("waiting() returned %d battles, want the one just created", len(waiting))
	}

	// Once it is active it leaves the lobby.
	if err := store.update(b.id, func(cur *battle) error {
		cur.status = "active"
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := store.waiting(); len(got) != 0 {
		t.Errorf("waiting() returned %d battles after the only one went active", len(got))
	}
}

// newTestBattle builds a one-sided battle, the shape create() takes.
func newTestBattle(t *testing.T, dex *pokedex, trainer, token string) *battle {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	names := fillTeam(dex, nil, rng)
	team, err := newCombatants(dex, names)
	if err != nil {
		t.Fatalf("building a team: %v", err)
	}
	return &battle{
		id:      randomID(rng, 6),
		status:  "waiting",
		version: 1,
		created: time.Now(),
		touched: time.Now(),
		sides:   []*side{{trainer: trainer, token: token, team: team}},
	}
}
