package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POKEDEX_TEST_DSN")
	if dsn == "" {
		t.Skip("POKEDEX_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// dropAll clears the schema so each test starts from nothing.
func dropAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	const stmt = `DROP TABLE IF EXISTS battle_sides, battles, trainers,
		pokemon_moves, moves, pokemon, schema_migrations CASCADE`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("dropping: %v", err)
	}
}

func TestMigrateCreatesSchema(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Every table the store writes to.
	for _, table := range []string{
		"pokemon", "moves", "pokemon_moves", "trainers", "battles", "battle_sides",
	} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			 WHERE table_name = $1)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s was not created", table)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	// Counted against the files on disk rather than a hardcoded 1:
	// the point is that migrating twice records each migration once,
	// which should stay true as migrations are added. Hardcoding it
	// meant the second migration ever written broke this test for
	// the wrong reason.
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	want := len(files)

	var count int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if count != want {
		t.Errorf("schema_migrations has %d rows, want %d - a migration ran twice", count, want)
	}
}

func TestMigrateConcurrentReplicas(t *testing.T) {
	pool := testPool(t)
	dropAll(t, pool)
	ctx := context.Background()

	const replicas = 4
	errs := make([]error, replicas)
	var wg sync.WaitGroup
	for i := range replicas {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = migrate(ctx, pool)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("replica %d: %v", i, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx,
		"SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("counting: %v", err)
	}
	// Derived, as above: concurrent starters must each record a
	// migration exactly once, however many there are.
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	if count != len(files) {
		t.Errorf("schema_migrations has %d rows, want %d", count, len(files))
	}
}
