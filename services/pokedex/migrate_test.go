package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMain drops the run's schema on the way out, so a run leaves the
// database as it found it. Tests that never touched postgres create
// nothing, and there is nothing to drop.
func TestMain(m *testing.M) {
	code := m.Run()
	if dsn := os.Getenv("POKEDEX_TEST_DSN"); dsn != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if pool, err := pgxpool.New(ctx, dsn); err == nil {
			// Best effort: the run's result is what matters, and a
			// stray schema is visible and harmless.
			_, _ = pool.Exec(ctx, "DROP SCHEMA IF EXISTS "+runSchema+" CASCADE")
			pool.Close()
		}
	}
	os.Exit(code)
}

// runSchema is this process's own schema, so a run neither sees nor
// disturbs anything outside itself. Every database test drops and
// recreates the tables it needs; against one shared schema, two runs
// tear down each other's tables halfway through.
//
// Named per process and created once: the tests share it within a run,
// which is what they expect, and nothing else can reach it.
var runSchema = fmt.Sprintf("test_%d_%d", os.Getpid(), time.Now().UnixNano()%1e6)

var schemaOnce sync.Once

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POKEDEX_TEST_DSN")
	if dsn == "" {
		t.Skip("POKEDEX_TEST_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing POKEDEX_TEST_DSN: %v", err)
	}
	// Nothing in the service qualifies a table name, so pointing the
	// search_path at our own schema is enough to redirect every query
	// in it without the tests or the migrations knowing.
	cfg.ConnConfig.RuntimeParams["search_path"] = runSchema

	schemaOnce.Do(func() {
		admin, err := pgxpool.New(ctx, dsn)
		if err != nil {
			t.Fatalf("connecting: %v", err)
		}
		defer admin.Close()
		if _, err := admin.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+runSchema); err != nil {
			t.Fatalf("creating %s: %v", runSchema, err)
		}
	})

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("pinging: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

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

	// Derived from disk, not a hardcoded number: new migrations must
	// not break this test.
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
	files, err := migrationFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	if count != len(files) {
		t.Errorf("schema_migrations has %d rows, want %d", count, len(files))
	}
}
