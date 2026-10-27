package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestMain owns the run's schema: created before any test, dropped
// after the last one, so a run leaves the database as it found it.
//
// Here rather than in testPool because a failure to set up is not a
// test failure - there is no test to fail yet, and t.Fatalf inside a
// sync.Once marks it done, so every later test would run against a
// schema that was never created and report something unrelated.
func TestMain(m *testing.M) {
	dsn := os.Getenv("POKEDEX_TEST_DSN")
	if dsn == "" {
		// No database, so the database tests skip and create nothing.
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connecting to POKEDEX_TEST_DSN: %v\n", err)
		cancel()
		os.Exit(1)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+runSchema); err != nil {
		fmt.Fprintf(os.Stderr, "creating %s: %v\n", runSchema, err)
		admin.Close()
		cancel()
		os.Exit(1)
	}
	cancel()

	code := m.Run()

	// Best effort: the run's result is what matters, and a schema left
	// behind is visible and harmless.
	dropCtx, dropCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, _ = admin.Exec(dropCtx, "DROP SCHEMA IF EXISTS "+runSchema+" CASCADE")
	dropCancel()
	admin.Close()
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

// emptyPool hands back a pool against a schema with none of the
// service's tables in it. Acquiring and emptying are one step because
// the tests share the run's schema: a test that acquired without
// emptying would read whatever the previous one left, which is a
// quiet wrong answer rather than a failure.
func emptyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testPool(t)
	ctx := context.Background()
	const stmt = `DROP TABLE IF EXISTS battle_sides, battles, trainers,
		pokemon_moves, moves, pokemon, schema_migrations CASCADE`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("emptying %s: %v", runSchema, err)
	}
	return pool
}

func TestMigrateCreatesSchema(t *testing.T) {
	pool := emptyPool(t)
	ctx := context.Background()

	if err := migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	for _, table := range []string{
		"pokemon", "moves", "pokemon_moves", "trainers", "battles", "battle_sides",
	} {
		var exists bool
		// to_regclass, not information_schema: that catalog spans every
		// schema the connection can see, so it answers yes for a table
		// in someone else's. This resolves through search_path, which
		// is what points at the run's own schema.
		err := pool.QueryRow(ctx,
			`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s was not created", table)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := emptyPool(t)
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
	pool := emptyPool(t)
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

// Per-run isolation works by pointing search_path at the run's own
// schema, which only redirects a query that names no schema itself. A
// migration saying "public.battles" would write to the developer's own
// data instead, and the suite would go back to destroying it - quietly,
// because every test would still pass.
func TestMigrationsNameNoSchema(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no migrations found; this test is looking in the wrong place")
	}
	qualified := regexp.MustCompile(`(?i)\b(public|pg_catalog|information_schema)\s*\.`)
	for _, f := range files {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(body), "\n") {
			// A -- comment can name a schema while talking about one;
			// only SQL can send a write to it.
			if sql, _, _ := strings.Cut(line, "--"); qualified.MatchString(sql) {
				t.Errorf("%s:%d names a schema, which search_path cannot redirect: %s",
					f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
