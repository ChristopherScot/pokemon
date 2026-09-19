package main

// Schema migrations, applied at startup.
//
// Two replicas start at the same time during a rollout, so this cannot
// assume it is alone. It takes a Postgres advisory lock first: the
// second pod blocks until the first finishes, then finds the migration
// already recorded and does nothing. Without the lock both would run
// CREATE TABLE concurrently and one would fail on a duplicate.
//
// Migrations are embedded rather than read from disk. The container is
// distroless and holds only the binary, so a file the image does not
// carry is a file that does not exist in the cluster.

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID is an arbitrary constant, unique to this service.
// Advisory locks share one namespace across the database, so a value
// another application also picked would make the two block on each
// other for no reason.
const migrationLockID int64 = 0x706f6b65 // "poke"

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the embedded .sql files, ordered by the numeric
// prefix rather than lexically: 10 sorts before 2 as a string.
func loadMigrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("reading migrations: %w", err)
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration %q is not named <version>_<name>.sql", e.Name())
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			return nil, fmt.Errorf("migration %q has a non-numeric version: %w", e.Name(), err)
		}
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", e.Name(), err)
		}
		out = append(out, migration{version: v, name: e.Name(), sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// migrate brings the schema up to date, and is safe to call from every
// replica at once.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return fmt.Errorf("no migrations embedded")
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring a connection to migrate: %w", err)
	}
	defer conn.Release()

	// Blocks rather than failing. A rollout starts pods together and
	// the loser should wait a moment, not crashloop.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("taking the migration lock: %w", err)
	}
	defer func() {
		// Best effort: the lock is released when the connection closes
		// anyway, so a failure here cannot wedge the next deploy.
		_, _ = conn.Exec(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	// The table the rest of this reads. Created outside the version
	// check because the check queries it.
	const bootstrap = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`
	if _, err := conn.Exec(ctx, bootstrap); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scanning applied migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		// One transaction per migration: a failure half way leaves the
		// schema as it was, rather than partly migrated with nothing
		// recorded.
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("beginning %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("applying %s: %w", m.name, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("recording %s: %w", m.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing %s: %w", m.name, err)
		}
	}
	return nil
}
