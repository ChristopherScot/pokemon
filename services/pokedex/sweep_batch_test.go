package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// The sweep deletes in bounded batches, and successive sweeps drain
// the backlog.
//
// Unbounded, a backlog turns one user's request into a full-table
// DELETE plus one FK cascade per row: measured at 193k expired
// battles, 992ms and 193,000 trigger calls, inside POST /battles and
// the lobby GET. A limit caps that at something a request can absorb
// while still draining, oldest first.
func TestTheSweepIsBoundedButStillDrains(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()
	_ = s

	// More than one batch of expired battles.
	//
	// Derived from the constant rather than fixed, so the test follows
	// it - but the assertion below compares against sweepBatchSize
	// itself, so raising the constant cannot make the test vacuous by
	// letting one batch swallow the whole backlog.
	const backlog = sweepBatchSize + 250
	for i := 0; i < backlog; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO battles (id, status, version, turn, turn_number, winner,
			                      created_at, touched_at, state)
			 VALUES ($1,'finished',1,0,0,'', now() - interval '5 hours',
			         now() - interval '5 hours', '{}'::jsonb)`,
			fmt.Sprintf("old%05d", i)); err != nil {
			t.Fatalf("seeding %d: %v", i, err)
		}
	}

	count := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM battles").Scan(&n); err != nil {
			t.Fatalf("counting: %v", err)
		}
		return n
	}
	if got := count(); got != backlog {
		t.Fatalf("seeded %d battles, want %d", got, backlog)
	}

	// One sweep takes a batch, not the lot.
	if _, err := store.waiting(ctx); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	afterOne := count()
	if afterOne == 0 {
		t.Fatalf("one sweep deleted the entire %d-row backlog: it is not bounded, "+
			"so a backlog lands in one user's request as a single DELETE", backlog)
	}
	if afterOne != backlog-sweepBatchSize {
		t.Errorf("after one sweep %d battles remain, want %d - the sweep should "+
			"delete at most %d so it cannot become an unbounded DELETE inside "+
			"someone's request", afterOne, backlog-sweepBatchSize, sweepBatchSize)
	}

	// And successive sweeps finish the job rather than stalling.
	for i := 0; i < 5 && count() > 0; i++ {
		if _, err := store.waiting(ctx); err != nil {
			t.Fatalf("sweep %d: %v", i+2, err)
		}
	}
	if got := count(); got != 0 {
		t.Errorf("%d expired battles survived repeated sweeps, want 0 - a bounded "+
			"sweep that does not drain is a leak", got)
	}
}

// Oldest first, so a backlog drains in a predictable order rather than
// leaving arbitrary rows behind indefinitely.
func TestTheSweepTakesTheOldestFirst(t *testing.T) {
	pool, store, dex := freshPG(t)
	s := service{dex: dex, battles: store, rng: rngFor(1)}
	ctx := context.Background()
	_ = s

	for i := 0; i < sweepBatchSize+10; i++ {
		// Older ids are older battles.
		age := time.Duration(sweepBatchSize+10-i)*time.Minute + 3*time.Hour
		if _, err := pool.Exec(ctx,
			`INSERT INTO battles (id, status, version, turn, turn_number, winner,
			                      created_at, touched_at, state)
			 VALUES ($1,'finished',1,0,0,'', now() - $2::interval, now() - $2::interval, '{}'::jsonb)`,
			fmt.Sprintf("age%05d", i), age.String()); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	if _, err := store.waiting(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	// The survivors should be the NEWEST ones, i.e. the highest ids.
	var oldestLeft string
	if err := pool.QueryRow(ctx,
		"SELECT id FROM battles ORDER BY touched_at LIMIT 1").Scan(&oldestLeft); err != nil {
		t.Fatalf("reading survivors: %v", err)
	}
	if oldestLeft < "age01000" {
		t.Errorf("oldest survivor is %s; the sweep should have taken the oldest "+
			"rows first so a backlog drains in order", oldestLeft)
	}
}
