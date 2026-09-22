package main

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
	return pool, newPGStore(pool), dex
}

func TestPGRegisterTrainer(t *testing.T) {
	_, store, _ := freshPG(t)

	token, err := store.registerTrainer(context.Background(), "Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	name, lookupErr := store.trainerByToken(context.Background(), token)
	if lookupErr != nil || name != "Ash" {
		t.Fatalf("trainerByToken = %q, %v; want Ash, nil", name, lookupErr)
	}

	// Case-insensitive, matching the in-memory store.
	if _, err := store.registerTrainer(context.Background(), "ash"); !errors.Is(err, errNameTaken) {
		t.Errorf("registering ash after Ash = %v, want errNameTaken", err)
	}
}

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
			_, err := store.registerTrainer(context.Background(), "misty")
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

func TestPGBattleSurvivesANewStore(t *testing.T) {
	pool, store, dex := freshPG(t)

	token, err := store.registerTrainer(context.Background(), "Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(context.Background(), b)

	other := newPGStore(pool)
	got, getErr := other.get(context.Background(), b.id)
	if getErr != nil {
		t.Fatalf("battle %s not found by a second store", b.id)
	}
	if got.Status != b.status || len(got.Sides) != len(b.sides) {
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

func TestPGConcurrentUpdatesAllLand(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer(context.Background(), "Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(context.Background(), b)

	const writers = 10
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = store.update(context.Background(), b.id, func(cur *battle) error {
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

	got, getErr := store.get(context.Background(), b.id)
	if getErr != nil {
		t.Fatal("battle disappeared")
	}
	if len(got.Log) != writers {
		t.Errorf("log has %d events after %d concurrent writers, want %d - "+
			"a write was lost", len(got.Log), writers, writers)
	}
	if got.Version < writers {
		t.Errorf("version is %d after %d updates, want at least %d",
			got.Version, writers, writers)
	}
}

func TestPGUpdatePropagatesClosureError(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer(context.Background(), "Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(context.Background(), b)

	sentinel := errors.New("nope")
	err = store.update(context.Background(), b.id, func(cur *battle) error {
		cur.status = "finished"
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("update returned %v, want the closure's error", err)
	}

	got, _ := store.get(context.Background(), b.id)
	if string(got.Status) == "finished" {
		t.Error("the mutation was committed despite the closure failing")
	}
}

func TestPGUpdateMissingBattle(t *testing.T) {
	_, store, _ := freshPG(t)
	err := store.update(context.Background(), "nosuchid", func(*battle) error { return nil })
	if !errors.Is(err, errNoBattle) {
		t.Errorf("update on a missing battle = %v, want errNoBattle", err)
	}
}

func TestPGWaitingLists(t *testing.T) {
	_, store, dex := freshPG(t)

	token, err := store.registerTrainer(context.Background(), "Ash")
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	b := newTestBattle(t, dex, "Ash", token)
	store.create(context.Background(), b)

	waiting, _ := store.waiting(context.Background())
	if len(waiting) != 1 || waiting[0].BattleId != b.id {
		t.Fatalf("waiting() returned %d battles, want the one just created", len(waiting))
	}

	// Once it is active it leaves the lobby.
	if err := store.update(context.Background(), b.id, func(cur *battle) error {
		cur.status = "active"
		return nil
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got, _ := store.waiting(context.Background()); len(got) != 0 {
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
		status:  api.BattleStatusWaiting,
		version: 1,
		created: time.Now(),
		touched: time.Now(),
		sides:   []*side{{trainer: trainer, token: token, team: team}},
	}
}
